# One public projection for UI, journal and diagnostics. No raw text fallback.
def enum($values; $fallback): . as $v | if $values|index($v) then $v else $fallback end;
def ascii_digits: type=="string" and length>0 and all(explode[]; .>=48 and .<=57);
def ascii_hex: type=="string" and length>0 and all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102));
# Entware jq may omit Oniguruma: keep strict formats without match/test/sub.
def timestamp:
  if type=="string" and length==20 and .[4:5]=="-" and .[7:8]=="-" and .[10:11]=="T" and
    .[13:14]==":" and .[16:17]==":" and .[19:20]=="Z" and
    ([.[0:4],.[5:7],.[8:10],.[11:13],.[14:16],.[17:19]] | all(.[]; ascii_digits)) then . else null end;
def operation_id:
  if type!="string" then null else . as $value | split("-") |
    if length==4 and .[0]=="op" and (.[1]|length)==14 and (.[1]|ascii_digits) and
      (.[2]|length)>0 and (.[2]|length)<=10 and (.[2]|ascii_digits) and
      (.[2]|tonumber)>1 and (.[2]|tonumber)<=2147483647 and
      (.[3]|length)==12 and (.[3]|ascii_hex) then $value else null end end;
def operation_type:
  if type!="string" then "unknown"
  elif startswith("subscriptions:") then "subscription_update"
  elif startswith("servers:") then "server_operation"
  elif .=="auto-switch" then "auto_switch"
  elif startswith("xray:") then "xray_maintenance"
  elif startswith("dot:") then "dns_operation"
  elif startswith("keenetic:") then "router_operation"
  elif . as $v | ["check","download","verify","plan","export","delete","resume"]|index($v) then "route_operation"
  else enum(["subscription_update","server_operation","auto_switch","xray_maintenance","dns_operation","router_operation","route_operation"];"unknown") end;
def source: enum(["USER","SCHEDULER","SUBSCRIPTION_AUTO","SERVER_CHECK_AUTO","AUTO_SWITCH","UPDATER","SYSTEM_RECOVERY"];"UNKNOWN");
def error_code: enum(["CANCELLED","OPERATION_FAILED","OWNER_DISAPPEARED","OWNER_CHANGED","OPERATION_BUSY","DOMAIN_OPERATION_BUSY","STATE_UNAVAILABLE","AUTOMATION_PAUSED","CHILDREN_UNCONFIRMED","CANCEL_NOT_SUPPORTED","OWNER_UNCONFIRMED","OWNER_PUBLICATION_FAILED"];null);
def operation_public:
  {operationId:(.operationId|operation_id),type:(.type|operation_type),source:(.source|source),
   state:(.state|enum(["starting","running","completed","failed","aborted","recovered"];"unknown")),
   phase:(.phase|enum(["starting","working","checking","fetching","parsing","committing","switching","waiting","recovering","finished"];"unknown")),
   running:(if .running|type=="boolean" then .running else null end),
   revision:(if .revision|type=="number" then .revision else null end),
   cancelability:(.cancelability|enum(["cooperative","protected"];"protected")),
   cancelRequested:(.cancelRequested==true),
   startedAt:(.startedAt|timestamp),updatedAt:(.updatedAt|timestamp),finishedAt:(.finishedAt|timestamp),
   errorCode:(.errorCode|error_code),
   ownerStatus:(.ownerStatus|enum(["ACTIVE","STALE","AMBIGUOUS"];"AMBIGUOUS")),
   ownerReason:(.ownerReason|enum(["identity_matches","absent","pid_reused","previous_boot","process_unreadable","identity_changed","invalid_identity"];"invalid_identity"))};
def event_name: enum(["started","lock_acquired","lock_conflict","phase_changed","cancel_requested","completed","failed","aborted","recovered","ambiguous_owner","heartbeat_problem","term","kill"];"unknown");
def event_message:
  if .=="started" then "Операция запущена"
  elif .=="lock_acquired" then "Ресурсы зарезервированы"
  elif .=="lock_conflict" then "Действие отложено: выполняется другая операция"
  elif .=="cancel_requested" then "Запрошена остановка операции"
  elif .=="completed" then "Операция завершена"
  elif .=="failed" then "Операция завершилась с ошибкой"
  elif .=="aborted" then "Операция прервана"
  elif .=="recovered" then "Незавершённая операция восстановлена"
  elif .=="ambiguous_owner" then "Состояние владельца не удалось подтвердить"
  elif .=="heartbeat_problem" then "Нет свежего сообщения о ходе операции"
  elif .=="term" then "Управляемому процессу отправлен сигнал завершения"
  elif .=="kill" then "Управляемый процесс принудительно завершён"
  elif .=="phase_changed" then "Этап операции изменился"
  else "Событие не распознано" end;
def event_public:
  (.event|event_name) as $event |
  {timestamp:(.timestamp|timestamp),operationId:(.operationId|operation_id),
   operationType:(.operationType|operation_type),source:(.source|source),event:$event,
   pid:(if (.pid|type)=="number" and .pid>1 and .pid<=2147483647 and .pid==(.pid|floor) then .pid else null end),
   result:(.result|enum(["success","failure","cancelled","pending","unknown"];"unknown")),
   errorCode:(.errorCode|error_code),message:($event|event_message)};
