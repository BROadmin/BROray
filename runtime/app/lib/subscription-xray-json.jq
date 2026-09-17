# Extract endpoint data only; never execute an imported Xray profile.
# Keep regex-free: Entware jq may be built without Oniguruma.
def require($ok): if $ok then . else error("unsupported-node") end;
def keys_only($keys): type=="object" and ((keys_unsorted - $keys)|length)==0;
def text_value: type=="string" and length<=4096 and all(explode[]; .>=32 and .!=127);
def unreserved: type=="string" and length>0 and length<=255 and
  all(explode[]; (.>=48 and .<=57) or (.>=65 and .<=90) or (.>=97 and .<=122) or .==45 or .==46 or .==95 or .==126);
def string_field($object;$key;$default):
  if $object|has($key) then $object[$key]|require(text_value) else $default end;
def object_field($object;$key):
  if $object|has($key) then $object[$key]|require(type=="object") else {} end;
def pair($key;$value): $key+"="+($value|@uri);
def decimal: type=="string" and length>0 and all(explode[]; .>=48 and .<=57);
def hex_group: type=="string" and length>0 and length<=4 and
  all(explode[]; (.>=48 and .<=57) or (.>=65 and .<=70) or (.>=97 and .<=102));
def ipv4_literal:
  split(".") | length==4 and all(.[]; decimal and length<=3 and
    (length==1 or (startswith("0")|not)) and (tonumber<=255));
def hex_groups: if .=="" then [] else split(":") | require(all(.[]; hex_group)) end;
def ipv6_literal:
  try (
    require(type=="string" and length>=2 and length<=45 and contains(":")) |
    # An IPv4 tail occupies two IPv6 groups. Zone IDs and bracket text are not accepted.
    (if contains(".") then
       (split(":")|last) as $tail | require($tail|ipv4_literal) |
       .[0:(length-($tail|length))]+"0:0"
     else . end) |
    split("::") |
    if length==1 then (.[0]|split(":")) | length==8 and all(.[]; hex_group)
    elif length==2 then (.[0]|hex_groups) as $left | (.[1]|hex_groups) as $right |
      (($left|length)+($right|length))<8
    else false end
  ) catch false;
def address_uri:
  require(type=="string" and length>0 and length<=255) |
  if startswith("[") or endswith("]") then
    require(startswith("[") and endswith("]")) | .[1:-1] |
    require(ipv6_literal) | "["+.+"]"
  elif contains(":") then require(ipv6_literal) | "["+.+"]"
  else require(unreserved) end;

def network:
  string_field(.;"network";"tcp") |
  if .=="raw" or .=="tcp" then "tcp"
  elif .=="websocket" or .=="ws" then "ws"
  elif .=="httpUpgrade" or .=="httpupgrade" then "httpupgrade"
  elif .=="splithttp" or .=="xhttp" then "xhttp"
  elif .=="grpc" then . else error("unsupported-node") end;
def only_transport($allowed):
  require(((keys_unsorted-["network","security","tlsSettings","realitySettings"])-$allowed)|length==0);
def aliased_object($primary;$legacy):
  require(((has($primary) and has($legacy))|not)) |
  if has($primary) then object_field(.;$primary) else object_field(.;$legacy) end;
def websocket_host:
  . as $w | object_field($w;"headers") as $headers |
  require($headers|all(keys[]; ascii_downcase=="host")) |
  ([$headers[]|require(text_value)]|unique) as $values |
  require(($values|length)<=1) |
  string_field($w;"host";"") as $host |
  require($host=="" or ($values|length)==0 or $host==$values[0]) |
  if $host!="" then $host else ($values[0] // "") end;

# Conservative XHTTP tuning subset. Unsupported stream/dialer/download settings
# remain rejected, not flattened into an independent endpoint.
def range_pair:
  if type=="number" then require(.==floor and .>=0 and .<=2147483647) | [.,.]
  elif type=="string" then split("-") |
    require((length==1 or length==2) and all(.[]; decimal and length<=10)) |
    map(tonumber) | require(all(.[]; .<=2147483647)) |
    if length==1 then [.[0],.[0]] else require(.[0]<=.[1]) end
  else error("unsupported-node") end;
def xhttp_extra_keys:
  ["headers","xPaddingBytes","noGRPCHeader","noSSEHeader","scMaxEachPostBytes",
   "scMinPostsIntervalMs","scMaxBufferedPosts","scStreamUpServerSecs","xmux"];
def xhttp_extra:
  require(keys_only(xhttp_extra_keys)) |
  require((tojson|length)<=4096) | . as $e |
  object_field($e;"headers") as $headers |
  require($headers|all(to_entries[]; (.key|text_value and length>0 and (ascii_downcase!="host")) and (.value|text_value))) |
  require(all(["noGRPCHeader","noSSEHeader"][]; . as $key | ($e|has($key)|not) or ($e[$key]|type)=="boolean")) |
  require(all(["xPaddingBytes","scMaxEachPostBytes","scMinPostsIntervalMs","scStreamUpServerSecs"][]; . as $key |
    ($e|has($key)|not) or ($e[$key]|range_pair|length)==2)) |
  require(($e|has("scMaxBufferedPosts")|not) or ($e.scMaxBufferedPosts|type=="number" and .==floor and .>=0 and .<=2147483647)) |
  object_field($e;"xmux") as $m |
  require($m|keys_only(["maxConcurrency","maxConnections","cMaxReuseTimes","hMaxRequestTimes","hMaxReusableSecs","hKeepAlivePeriod"])) |
  require(all(["maxConcurrency","maxConnections","cMaxReuseTimes","hMaxRequestTimes","hMaxReusableSecs"][]; . as $key |
    ($m|has($key)|not) or ($m[$key]|range_pair|length)==2)) |
  require(($m|has("hKeepAlivePeriod")|not) or ($m.hKeepAlivePeriod|type=="number" and .==floor and .>=0 and .<=2147483647)) |
  require(((($m.maxConcurrency // 0)|range_pair|.[1])>0 and (($m.maxConnections // 0)|range_pair|.[1])>0)|not) |
  require(($e|has("xPaddingBytes")|not) or ($e.xPaddingBytes|range_pair|.[0]>0 or .==[0,0])) |
  $e;
def xhttp_options:
  . as $x | require(keys_only(["host","path","mode","extra"]+xhttp_extra_keys)) |
  if has("extra") then
    # Xray replaces direct tuning with extra; do not silently choose between them.
    require((keys_unsorted-xhttp_extra_keys|length)==(keys_unsorted|length)) |
    object_field($x;"extra")|xhttp_extra
  else del(.host,.path,.mode)|xhttp_extra end;

def transport:
  . as $s | require(type=="object") | ($s|network) as $network |
  if $network=="grpc" then
    only_transport(["grpcSettings"]) |
    object_field($s;"grpcSettings") as $g |
    require($g|keys_only(["serviceName","authority","multiMode","mode"])) |
    require(($g|has("mode")|not) or $g.mode==false) |
    require(($g|has("multiMode")|not) or ($g.multiMode|type)=="boolean") |
    [pair("type";"grpc"),pair("serviceName";string_field($g;"serviceName";"")),
     pair("host";string_field($g;"authority";"")),pair("mode";if $g.multiMode==true then "multi" else "gun" end)]
  elif $network=="tcp" then
    only_transport(["tcpSettings","rawSettings"]) | aliased_object("rawSettings";"tcpSettings") as $t |
    require($t|keys_only(["header"])) |
    require(($t|has("header")|not) or ($t.header=={} or $t.header=={"type":"none"})) |
    [pair("type";"tcp")]
  elif $network=="ws" then
    only_transport(["wsSettings"]) | object_field($s;"wsSettings") as $w |
    require($w|keys_only(["path","host","headers"])) |
    [pair("type";"ws"),pair("path";string_field($w;"path";"/")),pair("host";($w|websocket_host))]
  elif $network=="httpupgrade" then
    only_transport(["httpupgradeSettings"]) | object_field($s;"httpupgradeSettings") as $u |
    require($u|keys_only(["path","host"])) |
    [pair("type";"httpupgrade"),pair("path";string_field($u;"path";"/")),pair("host";string_field($u;"host";""))]
  else
    only_transport(["xhttpSettings","splithttpSettings"]) | aliased_object("xhttpSettings";"splithttpSettings") as $x |
    ($x|xhttp_options) as $extra | string_field($x;"mode";"auto") as $mode |
    require(["","auto","packet-up","stream-up","stream-one"]|index($mode)!=null) |
    [pair("type";"xhttp"),pair("path";string_field($x;"path";"/")),pair("host";string_field($x;"host";"")),
     pair("mode";$mode),pair("extra";($extra|tojson))]
  end;

def protection:
  . as $s | string_field($s;"security";"none") as $security |
  require(["none","tls","reality"]|index($security)!=null) |
  if $security=="reality" then
    require((has("tlsSettings")|not)) |
    require(($s|network) as $network|["tcp","grpc","xhttp"]|index($network)!=null) |
    .realitySettings as $r |
    require($r|keys_only(["serverName","fingerprint","publicKey","password","shortId","spiderX","show"])) |
    require(($r|has("show")|not) or ($r.show|type)=="boolean") |
    require(($r.serverName|text_value and length>0)) |
    require((($r|has("publicKey") and has("password"))|not) or $r.publicKey==$r.password) |
    (if $r|has("password") then $r.password else $r.publicKey end) as $key |
    require($key|text_value and length>0) |
    [pair("security";"reality"),pair("sni";$r.serverName),pair("pbk";$key),
     pair("fp";string_field($r;"fingerprint";"chrome")),pair("sid";string_field($r;"shortId";"")),
     pair("spx";string_field($r;"spiderX";""))]
  elif $security=="tls" then
    require((has("realitySettings")|not)) | object_field($s;"tlsSettings") as $t |
    require($t|keys_only(["serverName","fingerprint","alpn","allowInsecure"])) |
    require(($t|has("allowInsecure")|not) or $t.allowInsecure==false) |
    require(($t|has("alpn")|not) or ($t.alpn|type=="array" and all(.[]; text_value and length>0 and length<=255 and (contains(",")|not)))) |
    [pair("security";"tls"),pair("sni";string_field($t;"serverName";"")),
     pair("fp";string_field($t;"fingerprint";"chrome")),pair("alpn";($t.alpn // []|join(",")))]
  else
    require((has("tlsSettings")|not) and (has("realitySettings")|not)) | [pair("security";"none")]
  end;

def endpoints:
  if has("vnext") then
    require(keys_only(["vnext"])) | require(.vnext|type=="array" and length>0) | .vnext[]
  else
    require(keys_only(["address","port","id","encryption","flow"])) |
    {address:.address,port:.port,users:[del(.address,.port)]}
  end;
def outbound_uris($profile;$index;$multiple):
  . as $out |
  require(keys_only(["protocol","tag","settings","streamSettings","fragment"]) and .protocol=="vless") |
  require(.settings|type=="object") | object_field($out;"streamSettings") as $stream |
  ($stream|transport) as $transport | ($stream|protection) as $protection |
  string_field($profile;"remarks";string_field($out;"tag";"Сервер")) as $label |
  ($label + (if $multiple then " / "+string_field($out;"tag";($index|tostring)) else "" end)) as $name |
  .settings|endpoints |
  require(keys_only(["address","port","users"])) | (.address|address_uri) as $address |
  require(.port|type=="number" and .==floor and .>=1 and .<=65535) |
  require(.users|type=="array" and length>0) | . as $endpoint | .users[] |
  require(keys_only(["id","encryption","flow"])) | require(.id|unreserved) |
  string_field(.;"encryption";"none") as $encryption | require($encryption=="none") |
  string_field(.;"flow";"") as $flow |
  require($flow=="" or ($flow=="xtls-rprx-vision" and ($stream|network)=="tcp" and $stream.security=="reality")) |
  "vless://"+.id+"@"+$address+":"+($endpoint.port|tostring)+"?"+
    (($transport+$protection+[pair("encryption";$encryption),pair("flow";$flow)])|join("&"))+"#"+($name|@uri);

# Slurp rejects concatenated documents. Buffer one bounded outbound so a later
# invalid user cannot leak a valid-looking partial expansion before the marker.
require(length==1) | .[0] |
(if type=="object" then [.] elif type=="array" then . else error("unsupported-document") end) |
require(all(.[]; type=="object" and (.outbounds|type)=="array" and all(.outbounds[]; type=="object"))) |
limit($max_nodes+1;
  .[] as $profile |
  ([$profile.outbounds[] | select(.protocol!="freedom" and .protocol!="blackhole")]) as $outbounds |
  $outbounds | to_entries[] | .key as $index | .value |
  try ([limit($max_nodes+1;outbound_uris($profile;$index+1;($outbounds|length)>1))][])
  catch "broray-json-error://unsupported-node"
)
