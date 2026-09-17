# Provider information only; no regex/Oniguruma dependency.
def trim: explode | until(length==0 or (.[0]!=32 and .[0]!=9 and .[0]!=13); .[1:]) | until(length==0 or (.[-1]!=32 and .[-1]!=9 and .[-1]!=13); .[:-1]) | implode;
def clean($n): type=="string" and length<=$n and all(explode[]; .>=32 and .!=127 and .!=65533);
def need($ok): if $ok then . else error("invalid-metadata") end;
def no_pad: split("=")[0];
def decode_text($n):
 need(clean(4096)) | if startswith("base64:") then
 .[7:] | explode | map(if .==45 then 43 elif .==95 then 47 else . end) | implode |
 . as $encoded | (no_pad) as $head | (length-($head|length)) as $pad |
 need(($head|length)>0 and ($head|length)%4!=1 and $pad<=2 and all(($head|explode)[]; (.>=65 and .<=90) or (.>=97 and .<=122) or (.>=48 and .<=57) or .==43 or .==47) and (if $pad>0 then length%4==0 and .==($head+(if $pad==1 then "=" else "==" end)) else true end)) |
 (if $head|length%4==2 then $head+"==" elif $head|length%4==3 then $head+"=" else $head end) |
 @base64d | need((@base64|no_pad)==$head)
 else . end | need(clean($n) and length>0);
def integer($maximum): trim | need(length>0 and length<=16 and all(explode[]; .>=48 and .<=57)) | tonumber | need(.>=0 and .<= $maximum and .==floor);
def link:
 decode_text(2048) | . as $v | need(startswith("https://") or startswith("http://")) |
 (split("://")[1]|split("/")[0]|split("?")[0]|split("#")[0]) as $authority |
 need(($authority|length)>0 and ($authority|contains("@")|not) and all(explode[]; .>32 and .!=127 and .!=34 and .!=39 and .!=60 and .!=62 and .!=92));
def usage:
 decode_text(512) | split(";") | map(trim | select(length>0) | split("=") | need(length==2) | {key:(.[0]|trim), value:(.[1]|trim)}) |
 need(length>0 and length<=4 and (map(.key)|unique|length)==length and all(.[]; .key=="upload" or .key=="download" or .key=="total" or .key=="expire")) |
 map(.value |= integer(9007199254740991)) | from_entries;
def known: ["profile-title","subscription-userinfo","profile-update-interval","support-url","profile-web-page-url","announce","announce-url","routing","routing-enable","update-always","custom-tunnel-config"];
def parse($key):
 if $key=="profile-title" then {title:decode_text(128)} elif $key=="announce" then {announcement:decode_text(1024)}
 elif $key=="support-url" then {supportUrl:link} elif $key=="profile-web-page-url" then {webPageUrl:link} elif $key=="announce-url" then {announcementUrl:link}
 elif $key=="subscription-userinfo" then {usage:usage} elif $key=="profile-update-interval" then decode_text(16) | integer(168) | need(.>=1) | {suggestedUpdateMinutes:(.*60)} else {ignoredDirectives:[$key]} end;
($previous[0] // {schemaVersion:1}) as $old | map(select(.name as $k | known|index($k)!=null)) | group_by(.name) |
reduce .[] as $group ($old; ($group[0].name) as $key | (try ($group | map(.value|trim) | unique | need(length==1) | .[0] | parse($key)) catch {invalidFields:[$key]}) as $new | . + $new + {invalidFields:((.invalidFields // [])+($new.invalidFields // [])|unique), ignoredDirectives:((.ignoredDirectives // [])+($new.ignoredDirectives // [])|unique)}) | .schemaVersion=1
