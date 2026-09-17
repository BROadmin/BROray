# Whitelist only model family and OS version; never unique device identifiers.
# No regex/Oniguruma dependency. Input is one sanitized show-version object.
def printable($n): type=="string" and length>0 and length<=$n and all(explode[]; .>=32 and .<=126);
def model_code: if type!="string" then "" else
 if length==7 and startswith("KN-") and all(.[3:]|explode[]; .>=48 and .<=57) then . else "" end end;
def version_text: if printable(64) and (.[0:1]|explode|all(.>=48 and .<=57)) and
 all(explode[]; (.>=48 and .<=57) or (.>=65 and .<=90) or (.>=97 and .<=122) or .==32 or .==46 or .==45 or .==43 or .==95 or .==40 or .==41)
 then . else "" end;
def model_from_name: if printable(128) then split("(")[-1]|split(")")[0]|model_code else "" end;
if type!="object" then {} else
 (.hw_id|model_code) as $hardware |
 (.model|model_from_name) as $model |
 ((.vendor=="Keenetic") or (.manufacturer=="Keenetic Ltd.") or (.manufacturer=="Keenetic Limited")) as $brand |
 # A current KN model also identifies Keenetic; an explicitly different vendor does not.
 (($brand or $hardware!="" or $model!="") and (.vendor==null or .vendor=="" or .vendor=="Keenetic" or .vendor=="Zyxel")) as $known |
 (if $hardware!="" and $model!="" and $hardware!=$model then "" elif $hardware!="" then $hardware else $model end) as $code |
 (.title|version_text) as $title | (.release|version_text) as $release |
 if $known then {os:"KeeneticOS"} + (if $code!="" then {model:$code} else {} end) +
 (if $title!="" then {osVersion:$title} elif $release!="" then {osVersion:$release} else {} end) else {} end end
