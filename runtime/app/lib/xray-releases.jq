# Entware jq may be built without oniguruma. Do not use test/match/sub.
def decimal: type=="string" and length>0 and all(explode[];.>=48 and .<=57);
def sha256: type=="string" and length==64 and all(explode[];(.>=48 and .<=57) or (.>=97 and .<=102));
def valid_tag: type=="string" and length<=80 and startswith("v") and
  all(explode[];(.>=48 and .<=57) or (.>=65 and .<=90) or (.>=97 and .<=122) or .==45 or .==46) and
  (ltrimstr("v")|split(".")|length>=3 and
    (.[0]|decimal and length<=4) and (.[1]|decimal and length<=2) and
    (.[2]|split("-")[0]|decimal and length<=2));
def normalize_release($asset; $digest):
  select(type == "object" and .draft == false and (.prerelease | type) == "boolean")
  | select(.tag_name | valid_tag)
  | .tag_name as $tag
  | [(.assets // [])[] | select(.name == $asset and .state == "uploaded" and
      (.size | type == "number" and . > 0) and (.id | type == "number"))] as $a
  | [(.assets // [])[] | select(.name == $digest and .state == "uploaded")] as $d
  | select(($a|length)==1 and ($d|length)==1)
  | select($a[0].browser_download_url == ("https://github.com/XTLS/Xray-core/releases/download/"+$tag+"/"+$asset))
  | select($d[0].browser_download_url == ("https://github.com/XTLS/Xray-core/releases/download/"+$tag+"/"+$digest))
  | select(.published_at | type == "string")
  | {tag_name:$tag, draft:false, prerelease, published_at,
     assets:([$a[0],$d[0]]|map({name,size,id,state,digest,browser_download_url}))};

def valid_record:
  try (type == "object" and (.candidateId|type=="string" and length>0) and
    (.architecture|type)=="string" and (.xrayTag|valid_tag) and
    (.archiveSha256|sha256) and
    (.testedAt|type=="string" and length>=20 and (split("T")[0]|split("-")|length==3 and all(.[];decimal))) and
    (.evidence|type=="string" and length>0) and
    (.status=="compatible" or .status=="incompatible")) catch false;
def compatibility($records; $context):
  . as $release
  | (.assets[0].digest // "" | ltrimstr("sha256:")) as $sha
  | [$records[] | select(valid_record) | select(
      .candidateId == $context.candidateId and .architecture == $context.architecture and
      .xrayTag == $release.tag_name and .archiveSha256 == $sha and ($sha | sha256) and
      (.testedAt | type == "string" and length > 0) and (.evidence | type == "string" and length > 0) and
      (.status == "compatible" or .status == "incompatible")
    )] | sort_by([(.status == "incompatible"), .testedAt]) | last
  | if . == null then {status:"untested",label:"Не проверялась на совместимость с BROray"}
    else . + {label:(if .status == "compatible" then "Совместима с BROray" else "Несовместима с BROray" end)} end;
def summarize($current):
  {tagName:.tag_name, version:(.tag_name|ltrimstr("v")), prerelease, publishedAt:.published_at,
   installed:(.tag_name == ("v"+$current)), available:true,
   archiveSha256:(.assets[0].digest // "" | ltrimstr("sha256:")),
   asset:{id:.assets[0].id,name:.assets[0].name,size:.assets[0].size,url:.assets[0].browser_download_url},
   digest:{name:.assets[1].name,url:.assets[1].browser_download_url},
   compatibility:.brorayCompatibility};
