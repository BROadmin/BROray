# Convert endpoint data only. Never execute or install an imported Xray profile.
# No regex dependency: Entware jq can be built without Oniguruma.
def require($ok): if $ok then . else error("unsupported-node") end;
def keys_only($keys): type=="object" and ((keys_unsorted - $keys)|length)==0;
def text_value: type=="string" and length<=4096 and all(explode[]; .>=32 and .!=127);
def optional_text($key): (has($key)|not) or (.[$key]|text_value);
def unreserved: type=="string" and length>0 and length<=255 and
  all(explode[]; (.>=48 and .<=57) or (.>=65 and .<=90) or (.>=97 and .<=122) or .==45 or .==46 or .==95 or .==126);
def string_field($object;$key;$default):
  if $object|has($key) then $object[$key]|require(text_value) else $default end;
def pair($key;$value): $key+"="+($value|@uri);

def transport:
  . as $s |
  require(keys_only(["network","security","grpcSettings","tcpSettings","rawSettings","tlsSettings","realitySettings"])) |
  ($s.network // "tcp") as $network |
  require(["grpc","tcp","raw"]|index($network)!=null) |
  if $network=="grpc" then
    require((has("tcpSettings")|not) and (has("rawSettings")|not)) |
    (.grpcSettings // {}) as $g |
    require($g|keys_only(["serviceName","authority","multiMode","mode"])) |
    # Some exported profiles carry mode:false. Only the canonical multiMode
    # boolean enables gRPC multi mode; unknown legacy values are rejected.
    require(($g|has("mode")|not) or $g.mode==false) |
    require(($g|has("multiMode")|not) or ($g.multiMode|type)=="boolean") |
    [pair("type";"grpc"), pair("serviceName";string_field($g;"serviceName";"")),
     pair("host";string_field($g;"authority";"")), pair("mode";if $g.multiMode==true then "multi" else "gun" end)]
  else
    require((has("grpcSettings")|not) and ((has("tcpSettings") and has("rawSettings"))|not)) |
    (.tcpSettings // .rawSettings // {}) as $t |
    require($t|keys_only(["header"])) |
    require(($t|has("header")|not) or ($t.header=={} or $t.header=={"type":"none"})) |
    [pair("type";"tcp")]
  end;

def protection:
  . as $s | (.security // "none") as $security |
  require(["none","tls","reality"]|index($security)!=null) |
  if $security=="reality" then
    require((has("tlsSettings")|not)) |
    .realitySettings as $r |
    require($r|keys_only(["serverName","fingerprint","publicKey","shortId","spiderX","show"])) |
    require(($r|has("show")|not) or ($r.show|type)=="boolean") |
    require(($r.serverName|text_value and length>0) and ($r.publicKey|text_value and length>0)) |
    [pair("security";"reality"), pair("sni";$r.serverName), pair("pbk";$r.publicKey),
     pair("fp";string_field($r;"fingerprint";"chrome")), pair("sid";string_field($r;"shortId";"")),
     pair("spx";string_field($r;"spiderX";""))]
  elif $security=="tls" then
    require((has("realitySettings")|not)) |
    (.tlsSettings // {}) as $t |
    require($t|keys_only(["serverName","fingerprint","alpn","allowInsecure"])) |
    # BROray's generator does not carry allowInsecure into TLS. Reject requests
    # to disable certificate verification instead of silently changing them.
    require(($t|has("allowInsecure")|not) or $t.allowInsecure==false) |
    require(($t|has("alpn")|not) or ($t.alpn|type=="array" and all(.[]; text_value and (contains(",")|not)))) |
    [pair("security";"tls"), pair("sni";string_field($t;"serverName";"")),
     pair("fp";string_field($t;"fingerprint";"chrome")), pair("alpn";($t.alpn // []|join(",")))]
  else
    require((has("tlsSettings")|not) and (has("realitySettings")|not)) | [pair("security";"none")]
  end;

def outbound_uris($profile;$index;$multiple):
  . as $out |
  require(keys_only(["protocol","tag","settings","streamSettings","fragment"]) and .protocol=="vless") |
  require(.settings|keys_only(["vnext"])) |
  require(.settings.vnext|type=="array" and length>0) |
  (.streamSettings // {}) as $stream |
  ($stream|transport) as $transport | ($stream|protection) as $protection |
  (string_field($profile;"remarks";string_field($out;"tag";"Сервер"))) as $label |
  ($label + (if $multiple then " / "+string_field($out;"tag";($index|tostring)) else "" end)) as $name |
  .settings.vnext[] |
  require(keys_only(["address","port","users"])) |
  require(.address|unreserved) |
  require(.port|type=="number" and .==floor and .>=1 and .<=65535) |
  require(.users|type=="array" and length>0) |
  . as $endpoint | .users[] |
  require(keys_only(["id","encryption","flow"])) |
  require(.id|unreserved) |
  (string_field(.;"encryption";"none")) as $encryption |
  require($encryption=="none") |
  (string_field(.;"flow";"")) as $flow |
  require($flow=="" or ($flow=="xtls-rprx-vision" and ($stream.network // "tcp")!="grpc" and $stream.security=="reality")) |
  "vless://"+.id+"@"+$endpoint.address+":"+($endpoint.port|tostring)+"?"+
    (($transport+$protection+[pair("encryption";$encryption),pair("flow";$flow)])|join("&"))+"#"+($name|@uri);

# Slurp mode rejects two concatenated documents, even if each is valid JSON.
require(length==1) | .[0] |
(if type=="object" then [.] elif type=="array" then . else error("unsupported-document") end) |
require(all(.[]; type=="object" and (.outbounds|type)=="array" and all(.outbounds[]; type=="object"))) |
limit($max_nodes+1;
  .[] as $profile |
  ([$profile.outbounds[] | select(.protocol!="freedom" and .protocol!="blackhole")]) as $outbounds |
  $outbounds | to_entries[] |
  .key as $index | .value |
  try outbound_uris($profile;$index+1;($outbounds|length)>1)
  catch "broray-json-error://unsupported-node"
)
