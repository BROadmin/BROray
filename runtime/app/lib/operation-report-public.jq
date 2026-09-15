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
