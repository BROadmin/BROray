include "operation-public";
def version_value:
  if type=="string" and length<=24 then . as $v | split(".") |
    if length==3 and all(.[]; ascii_digits and length<=6) then $v else null end else null end;
def candidate_value:
  if type=="string" and length<=48 then . as $v | split("-") |
    if length==2 and (.[0]|version_value)!=null and (.[1]|startswith("r")) then
      .[1][1:] | split("c") | if (length==1 or length==2) and all(.[]; ascii_digits and length<=6) then $v else null end
    else null end else null end;
def webui_value:
  if type=="string" and startswith("WebUI-") and (.[6:]|candidate_value)!=null then . else null end;
def build_public:
  {appVersion:(.appVersion|version_value),candidateId:(.candidateId|candidate_value),webuiBuild:(.buildId|webui_value)};

def diagnostic_pid:
  if type=="number" and .>1 and .<=2147483647 and .==floor then . else null end;

def updater_id:
  if type!="string" or length>96 then null else . as $v | split("-") |
    if length==3 and (.[0]=="update" or .[0]=="reinstall") and
      (.[1]|length)==14 and (.[1]|ascii_digits) and (.[2]|ascii_digits) and
      (.[2]|length)<=10 then $v else null end end;

def updater_public:
  if type!="object" then null else
    (.operationId|updater_id) as $id |
    (.state|enum(["queued","running","success","failed","error"];null)) as $state |
    if $id==null or $state==null or (.running|type)!="boolean" then null else
      {operationId:$id,state:$state,running:.running,
       operation:(.operation|enum(["update","reinstall"];null)),updatedAt:(.updatedAt|timestamp),
       rollbackPerformed:(if (.rollbackPerformed|type)=="boolean" then .rollbackPerformed else null end)}
    end end;
