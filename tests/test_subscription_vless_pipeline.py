"""Actual Linux extraction, node staging, URI parsing, config generation and Xray -test.

All input is synthetic. No HTTP transport, provider, real router or proxy process.
The optional core gate is required when BRORAY_TEST_XRAY is supplied. It only
validates generated JSON (run -test), never starts listening or connects to peers.
"""
from __future__ import annotations
import base64
import copy
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from test_subscription_vless_compat import profile, stream, outbound, user
from urllib.parse import quote, urlencode

ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
XRAY=os.environ.get('BRORAY_TEST_XRAY','')
CORE_RESULTS=[]

class Pipeline(unittest.TestCase):
    def setUp(self):
        self.temp=Path(tempfile.mkdtemp(prefix='broray-vless-stage05-'))
        self.app=self.temp/'app'
        shutil.copytree(ROOT/'runtime/app/lib',self.app/'lib')
        for d in ['tmp','servers','config/system','logs']: (self.app/d).mkdir(parents=True,exist_ok=True)
        settings={'listenAddress':'127.0.0.1','socksPort':2080,'logLevel':'warning'}
        (self.app/'config/system/settings.json').write_text(json.dumps(settings),encoding='utf-8')
        self.env={**os.environ,'BRORAY_BASE':str(self.app),'BRORAY_ROOT':str(self.app),
            'BRORAY_SUB_BASE':str(self.app),'PATH':'/usr/bin:/bin:/usr/sbin:/sbin'}
    def tearDown(self):
        assert self.temp.parent.resolve()==Path(tempfile.gettempdir()).resolve()
        assert self.temp.name.startswith('broray-vless-stage05-')
        shutil.rmtree(self.temp)
    def shell(self,script,*args):
        return subprocess.run(['/bin/ash','-c',script,'stage05',*map(str,args)],env=self.env,
                              capture_output=True,timeout=45)
    def extract(self,payload):
        raw=payload if isinstance(payload,bytes) else json.dumps(payload,ensure_ascii=False).encode()
        file=self.app/'input';file.write_bytes(raw)
        p=self.shell('''. "$BRORAY_ROOT/lib/subscription-service.sh"
rc=0
broray_subscription_extract_nodes "$BRORAY_ROOT/input" "$BRORAY_ROOT/nodes" || rc=$?
if [ "$rc" = 0 ]; then
 broray_subscription_stage_nodes fixture "$BRORAY_ROOT/nodes" "$BRORAY_ROOT/stage" true || rc=$?
fi
jq -nc --argjson rc "$rc" --arg code "${BRORAY_SUB_ERROR_CODE:-}" \
 --argjson received "${BRORAY_SUB_RECEIVED:-0}" --argjson accepted "${BRORAY_SUB_ACCEPTED:-0}" \
 --argjson rejected "${BRORAY_SUB_REJECTED:-0}" \
 '{rc:$rc,errorCode:$code,received:$received,accepted:$accepted,rejected:$rejected}'
''')
        self.assertEqual(p.returncode,0,p.stderr.decode(errors='replace'))
        result=json.loads(p.stdout)
        nodes=[json.loads(f.read_bytes()) for f in sorted((self.app/'stage').glob('*.json'))]
        return result,nodes

    def test_uri_control_credentials_are_rejected_before_save(self):
        for encoded in ['abc%0A','a%00b','abc%0a','abc%0A%0a']:
            with self.subTest(encoded=encoded):
                result,nodes=self.extract(('trojan://'+encoded+'@vpn.example.invalid:443?security=tls').encode())
                self.assertEqual(result['accepted'],0,(result,nodes))
                self.assertEqual(nodes,[])

    def test_uri_decoder_preserves_printable_components(self):
        for encoded,expected in [('a+b','a+b'),('a%2Bb','a+b'),('a%20b','a b'),('a%250Ab','a%0Ab'),('a%5Cb','a\\b')]:
            result,nodes=self.extract(('trojan://'+encoded+'@vpn.example.invalid:443?security=tls').encode())
            self.assertEqual(result['accepted'],1,result)
            self.assertEqual(nodes[0]['password'],expected)
    def test_uri_decoder_preserves_formatted_json(self):
        extra={'noSSEHeader':True}
        n,c=self.one(self.trojan_uri('type=xhttp&extra='+quote(json.dumps(extra,indent=2))))
        self.assertEqual(n['transport']['extra'],extra)
        self.assertEqual(c['streamSettings']['xhttpSettings']['extra'],extra)
    def test_uri_decoder_rejects_loss_before_output(self):
        for encoded in ['abc%00def','abc%0A','abc\n','a%GG']:
            p=self.shell('. "$BRORAY_ROOT/lib/util.sh"; broray_uri_component_decode "$1"',encoded)
            self.assertNotEqual(p.returncode,0);self.assertEqual(p.stdout,b'')
        for encoded,expected in [('a%0Ab',b'a\nb'),('a%09b',b'a\tb')]:
            p=self.shell('. "$BRORAY_ROOT/lib/util.sh"; value="$(broray_uri_component_decode "$1")" || exit 1; printf "%s" "$value"',encoded)
            self.assertEqual(p.returncode,0,p.stderr);self.assertEqual(p.stdout,expected)
    def generate(self,node):
        (self.app/'servers'/f"{node['id']}.json").write_text(json.dumps(node),encoding='utf-8')
        p=self.shell('. "$BRORAY_ROOT/lib/server-config-generator.sh"; broray_generate_server_config "$1"',node['id'])
        self.assertEqual(p.returncode,0,p.stderr.decode(errors='replace'))
        file=Path(p.stdout.decode().strip());self.assertTrue(file.is_relative_to(self.app))
        config=json.loads(file.read_bytes())
        self.assertEqual(config['inbounds'][0]['listen'],'127.0.0.1')
        self.assertEqual(len(config['outbounds']),1)
        self.assertNotIn('dns',config);self.assertNotIn('routing',config)
        if XRAY:
            core=subprocess.run([XRAY,'run','-test','-config',str(file)],capture_output=True,timeout=20,env=self.env)
            text=(core.stdout+core.stderr).decode(errors='replace')
            CORE_RESULTS.append({'case':self.id(),'network':node['network'],'security':node['security'],
                'configurationSha256':hashlib.sha256(file.read_bytes()).hexdigest(),'returncode':core.returncode})
            self.assertEqual(core.returncode,0,text)
            self.assertIn('Configuration OK',text)
        return config['outbounds'][0]
    def one(self,payload):
        result,nodes=self.extract(payload)
        self.assertEqual(result['rc'],0,result);self.assertEqual(result['accepted'],1,result)
        self.assertEqual(len(nodes),1)
        self.assertEqual(nodes[0]['source']['subscriptionId'],'fixture')
        return nodes[0],self.generate(nodes[0])
    def audit_vmess(self, **kw):
        data=dict(v="2",ps="audit",add="vpn.example.invalid",port="443",
                  id="11111111-2222-4333-8444-555555555555",aid=0,scy="auto",net="tcp",tls="tls")
        data.update(kw)
        return ("vmess://"+base64.b64encode(json.dumps(data).encode()).decode()).encode()
    def test_combinations_hy2_optional_auth(self):
        for uri in [b"hy2://vpn.example.invalid",b"hysteria2://@vpn.example.invalid/",b"hy2://[2001:db8::1]"]:
            n,c=self.one(uri)
            self.assertEqual(n["auth"],"");self.assertEqual(n["port"],443)
            self.assertEqual(c["streamSettings"]["hysteriaSettings"]["auth"],"")
    def test_combinations_hy2_gecko_hopping(self):
        n,c=self.one(b"hy2://secret@vpn.example.invalid:443,5000-5010/?obfs=gecko&obfs-password=mask")
        self.assertEqual(n["port"],443)
        self.assertEqual(c["streamSettings"]["finalmask"],{"udp":[
            {"type":"salamander","settings":{"password":"mask","packetSize":"512-1200"}},
            {"type":"udphop","settings":{"mode":"intervalremote","interval":30,"remotePorts":"443,5000-5010"}}]})
        n,c=self.one(b"hy2://[2001:db8::1]:5000-5010/")
        self.assertEqual(n["port"],5000)
        self.assertEqual(c["streamSettings"]["finalmask"]["udp"][0]["settings"]["remotePorts"],"5000-5010")
    def test_combinations_hy2_hopping_rejections(self):
        for port in ["0,443","443,65536","443,","443,,444","500-400","-443","abc","443x"]:
            self.reject_uri(("hy2://host.invalid:"+port).encode())
        for obfs in ["gecko","gecko&obfs-password=","other&obfs-password=p"]:
            self.reject_uri(("hy2://host.invalid?obfs="+obfs).encode())
        for mask in [{"udp":[{"type":"salamander","settings":{"password":"foreign"}}]},
                     {"udp":[{"type":"udphop","settings":{"mode":"intervalremote","interval":30,"remotePorts":"444"}}]}]:
            self.reject_uri(("hy2://host.invalid:443,444?obfs=gecko&obfs-password=p&"+urlencode({"fm":json.dumps(mask)},quote_via=quote)).encode())
    def test_combinations_hy2_mask_composition(self):
        fm={"udp":[{"type":"header-custom","settings":{"clients":[[{"packet":[1,2,3]}]]}}]}
        n,c=self.one(("hy2://secret@host.invalid:443,444?obfs=salamander&obfs-password=p&"+urlencode({"fm":json.dumps(fm)},quote_via=quote)).encode())
        masks=c["streamSettings"]["finalmask"]["udp"]
        self.assertEqual([m["type"] for m in masks],["salamander","header-custom","udphop"])
        self.assertEqual(masks[1],fm["udp"][0])
    def test_combinations_xhttp_download(self):
        for security in ["tls","reality"]:
            p=profile("xhttp",security);stream(p)["xhttpSettings"]["mode"]="stream-up"
            download=stream(profile("xhttp",security))
            download.update(address="download.invalid",port=8443)
            extra={"downloadSettings":download,"noSSEHeader":True}
            stream(p)["xhttpSettings"]["extra"]=extra
            n,c=self.one(p)
            self.assertEqual(c["streamSettings"]["xhttpSettings"]["extra"],extra)
            n,c=self.one(self.trojan_uri("type=xhttp&mode=stream-up&"+urlencode({"extra":json.dumps(extra)},quote_via=quote)))
            self.assertEqual(c["streamSettings"]["xhttpSettings"]["extra"],extra)
    def test_combinations_xhttp_tuning(self):
        extra={"xPaddingBytes":"100-200","xPaddingObfsMode":True,"xPaddingKey":"pad",
               "xPaddingHeader":"X-Pad","xPaddingPlacement":"header","xPaddingMethod":"tokenish",
               "uplinkHTTPMethod":"GET","sessionIDPlacement":"cookie","sessionIDKey":"session",
               "sessionIDTable":"hex","sessionIDLength":"16-20","seqPlacement":"query","seqKey":"seq",
               "uplinkDataPlacement":"header","uplinkDataKey":"X-Data","uplinkChunkSize":"100-200",
               "serverMaxHeaderBytes":8192}
        p=profile("xhttp");stream(p)["xhttpSettings"].update(mode="packet-up",extra=extra)
        for payload in [p,self.vless_uri("type=xhttp&mode=packet-up&security=tls&"+urlencode({"extra":json.dumps(extra)},quote_via=quote)),
                        self.audit_vmess(net="xhttp",type="packet-up",extra=extra)]:
            n,c=self.one(payload);self.assertEqual(c["streamSettings"]["xhttpSettings"]["extra"],extra)
    def test_combinations_xhttp_invalid(self):
        for extra in [{"downloadSettings":{"network":"tcp"}},
                      {"downloadSettings":{"network":"xhttp","sockopt":{"dialerProxy":"foreign"}}},
                      {"downloadSettings":{"network":"xhttp","xhttpSettings":{"extra":{"downloadSettings":{"network":"xhttp"}}}}},
                      {"sessionIDTable":"hex","sessionIDLength":1},{"sessionIDTable":"é","sessionIDLength":20},
                      {"uplinkHTTPMethod":"GET"},{"uplinkDataPlacement":"cookie"},{"seqPlacement":"bad"},
                      {"xPaddingMethod":"bad"},{"serverMaxHeaderBytes":-1}]:
            self.reject_uri(self.vless_uri("type=xhttp&security=tls&"+urlencode({"extra":json.dumps(extra)},quote_via=quote)))
        download=stream(profile("xhttp"))
        self.reject_uri(self.vless_uri("type=xhttp&mode=stream-one&security=tls&"+urlencode({"extra":json.dumps({"downloadSettings":download})},quote_via=quote)))
    def test_combinations_raw_full_header(self):
        header={"type":"http","request":{"method":"POST","version":"1.1","path":["/a","/b"],
                    "headers":{"Host":["front.invalid"],"User-Agent":["test-agent"],"X-Test":["a+b"]}},
                "response":{"status":"200","reason":"OK","version":"1.1","headers":{"X-Reply":["yes"]}}}
        p=profile("raw");stream(p)["rawSettings"]["header"]=header
        n,c=self.one(p);self.assertEqual(c["streamSettings"]["rawSettings"]["header"],header)
        self.assertEqual(n["transport"]["header"],header)
        for q in ["type=ws", "type=raw&host=conflict", "type=raw&path=/conflict"]:
            self.reject_uri(self.vless_uri(q+"&security=tls&"+urlencode({"header":json.dumps(header)},quote_via=quote)))
        for bad in [{"type":"http","request":{"headers":{"Host":None}}},{"type":"http","request":{"unknown":1}},
                    {"type":"none","request":{"method":"POST"}}]:
            self.reject_uri(self.vless_uri("security=tls&"+urlencode({"header":json.dumps(bad)},quote_via=quote)))
    def json_protocol(self,protocol,network="raw",security="tls",flat=False):
        p=profile(network,security);o=outbound(p);o["protocol"]=protocol
        endpoint={"address":"vpn.example.invalid","port":443}
        if protocol=="vmess":
            account={"id":"11111111-2222-4333-8444-555555555555","security":"chacha20-poly1305","alterId":0}
            o["settings"]=(endpoint|account) if flat else {"vnext":[endpoint|{"users":[account]}]}
        else:
            endpoint["password"]="secret+/@:#%"
            if protocol=="shadowsocks":endpoint["method"]="aes-128-gcm"
            o["settings"]=endpoint if flat else {"servers":[endpoint]}
        return p
    def test_combinations_json_vmess(self):
        for net,sec,flat in [("raw","tls",True),("ws","tls",False),("grpc","tls",False),("xhttp","reality",False),("kcp","none",False)]:
            p=self.json_protocol("vmess",net,sec,flat)
            if net=="kcp":stream(p)["kcpSettings"]={"mtu":1350,"tti":20}
            n,c=self.one(p);self.assertEqual(n["protocol"],"vmess")
            self.assertEqual(c["settings"]["vnext"][0]["users"][0]["security"],"chacha20-poly1305")
    def test_combinations_json_trojan(self):
        for net,sec,flat in [("raw","tls",True),("grpc","reality",False),("xhttp","tls",False),("httpupgrade","tls",False)]:
            p=self.json_protocol("trojan",net,sec,flat)
            if net=="xhttp":stream(p)["xhttpSettings"]["extra"]={"noSSEHeader":True}
            n,c=self.one(p);self.assertEqual(n["password"],"secret+/@:#%")
            self.assertEqual(c["settings"]["servers"][0]["password"],n["password"])
    def test_combinations_json_shadowsocks(self):
        for flat,method,password in [(False,"aes-128-gcm","secret+/@:#%"),(True,"2022-blake3-aes-128-gcm",base64.b64encode(b"a"*16).decode())]:
            p=self.json_protocol("shadowsocks","raw","none",flat)
            st=outbound(p)["settings"];endpoint=st if flat else st["servers"][0]
            endpoint.update(method=method,password=password)
            n,c=self.one(p);self.assertEqual(n["password"],password);self.assertEqual(n["method"],method)
            self.assertEqual(c["protocol"],"shadowsocks")
    def test_combinations_json_atomic_rejection(self):
        p=self.json_protocol("vmess");users=outbound(p)["settings"]["vnext"][0]["users"]
        users.append(dict(users[0],alterId=1))
        r,n=self.extract(p);self.assertEqual(r["accepted"],0);self.assertEqual(n,[])
        for protocol in ["vmess","trojan","shadowsocks"]:
            p=self.json_protocol(protocol,"raw","none" if protocol=="shadowsocks" else "tls")
            outbound(p)["proxySettings"]={"tag":"foreign"}
            r,n=self.extract(p);self.assertEqual(r["accepted"],0)
        p=self.json_protocol("shadowsocks","ws","tls")
        r,n=self.extract(p);self.assertEqual(r["accepted"],0)
    def test_combinations_json_mixed_profile(self):
        p=profile();p["outbounds"] += [outbound(self.json_protocol("vmess")),outbound(self.json_protocol("trojan")),outbound(self.json_protocol("shadowsocks","raw","none"))]
        p.update(dns={"servers":["foreign.invalid"]},routing={"domainStrategy":"AsIs"},inbounds=[{"port":9999}])
        r,nodes=self.extract(p);self.assertEqual(r["accepted"],4,r)
        self.assertEqual({n["protocol"] for n in nodes},{"vless","vmess","trojan","shadowsocks"})
        for n in nodes:self.generate(n)
    def test_combinations_legacy_vmess_has_no_prior_header(self):
        header={"type":"http","request":{"headers":{"Host":["previous.invalid"]}}}
        first=self.vless_uri("security=tls&"+urlencode({"header":json.dumps(header)},quote_via=quote)).decode()
        second=self.audit_vmess().decode()
        r=self.shell('. "$BRORAY_ROOT/lib/server-import.sh"; broray_server_import_dispatch "$1" subscription fixture 1; broray_server_import_dispatch "$2" subscription fixture 2',first,second)
        self.assertEqual(r.returncode,0,r.stderr)
        n=json.loads((self.app/"servers/subscription-fixture-0002.json").read_bytes())
        self.assertNotIn("header",n["transport"])
        c=self.generate(n);self.assertEqual(c["streamSettings"]["rawSettings"]["header"],{"type":"none"})
    def test_combinations_new_fields_dedupe(self):
        pairs=[]
        for key in ["Host","User-Agent"]:
            pairs.append([self.vless_uri("security=tls&"+urlencode({"header":json.dumps({"type":"http","request":{"headers":{key:[v]}}})},quote_via=quote)) for v in ["a.invalid","b.invalid"]])
        pairs.append([("hy2://host.invalid:"+ports).encode() for ports in ["443,5000","443,5001"]])
        pairs.append([("hy2://host.invalid?obfs="+obfs+"&obfs-password=p").encode() for obfs in ["salamander","gecko"]])
        pairs.append([self.vless_uri("type=xhttp&security=tls&"+urlencode({"extra":json.dumps({"sessionIDKey":v})},quote_via=quote)) for v in ["a","b"]])
        for pair in pairs:
            r,nodes=self.extract(b"\n".join(pair));self.assertEqual(r["accepted"],2,r)
            for n in nodes:self.generate(n)
        r,nodes=self.extract(b"\n".join([pairs[0][0],pairs[0][0]]))
        self.assertEqual(r["accepted"],1,r);self.assertEqual(r["rejected"],1,r)
    def test_combinations_finalmask_tls_reality(self):
        fm={"tcp":[{"type":"fragment","settings":{"packets":"tlshello","length":"100-200","delay":"10-20"}},
                   {"type":"header-custom","settings":{"clients":[[{"packet":[1,2,3]}]]}}]}
        for protocol in ["vless","vmess","trojan"]:
            for security in ["tls","reality"]:
                p=profile("raw",security) if protocol=="vless" else self.json_protocol(protocol,"raw",security)
                stream(p)["finalmask"]=fm
                n,c=self.one(p);self.assertEqual(c["streamSettings"]["finalmask"],fm)
    def test_audit_tls_fields_preserved(self):
        values=dict(ech="cloudflare-ech.com",pcs="ab"*32,vcn="cert.invalid")
        q=urlencode(values)
        for uri in [self.vless_uri("security=tls&"+q),self.trojan_uri(q),
                    self.audit_vmess(**values), ("hy2://auth@vpn.example.invalid?"+q).encode()]:
            with self.subTest(uri=uri.split(b":")[0]):
                n,c=self.one(uri)
                for key,value in [("echConfigList",values["ech"]),("pinnedPeerCertSha256",values["pcs"]),("verifyPeerCertByName",values["vcn"])]:
                    self.assertEqual(n["tls"].get(key),value)
                    self.assertEqual(c["streamSettings"]["tlsSettings"].get(key),value)
    def test_audit_vless_reality_pqv(self):
        pqv="A"*2603
        n,c=self.one(self.vless_uri(urlencode(dict(security="reality",sni="front.invalid",pbk="A"*43,pqv=pqv))))
        self.assertEqual(n["reality"].get("mldsa65Verify"),pqv)
        self.assertEqual(c["streamSettings"]["realitySettings"].get("mldsa65Verify"),pqv)
        self.reject_uri(self.vless_uri("security=reality&sni=front.invalid&pbk="+"A"*43+"&pqv=bad"))
        self.reject_uri(self.vless_uri("security=tls&pqv="+pqv))
    def test_audit_finalmask_preserved(self):
        fm={"tcp":[{"type":"fragment","settings":{"packets":"tlshello","length":"100-200","delay":"10-20"}}]}
        for uri in [self.vless_uri("security=tls&"+urlencode(dict(fm=json.dumps(fm)),quote_via=quote)),
                    self.trojan_uri(urlencode(dict(fm=json.dumps(fm)),quote_via=quote)),self.audit_vmess(fm=fm)]:
            n,c=self.one(uri)
            self.assertEqual(c["streamSettings"].get("finalmask"),fm)
        for fm in ["{broken","[]","null",'{"tcp":[{"type":"header-http"}]}']:
            self.reject_uri(self.vless_uri("security=tls&"+urlencode(dict(fm=fm))))
    def test_audit_trojan_root_and_ipv6(self):
        for uri,address in [(b"trojan://secret@vpn.example.invalid:443/?security=tls","vpn.example.invalid"),
                            (b"trojan://secret@[2001:db8::1]:443/?security=tls","2001:db8::1")]:
            n,c=self.one(uri);self.assertEqual(n["address"],address);self.assertEqual(n["port"],443)
        self.reject_uri(b"trojan://secret@vpn.example.invalid:443/non-root?security=tls")
    def test_audit_trojan_ambiguous_query(self):
        for q in ["sni=a&sni=b","type=grpc&authority=a&host=b","serviceName=a&service_name=b",
                  "security=tls&security=reality","allowInsecure=false&allowInsecure=true","%73ni=a&sni=b"]:
            self.reject_uri(self.trojan_uri(q))
        n,c=self.one(self.trojan_uri("type=grpc&authority=front.invalid&host=front.invalid"))
        self.assertEqual(c["streamSettings"]["grpcSettings"]["authority"],"front.invalid")
    def test_audit_trojan_raw_header(self):
        n,c=self.one(self.trojan_uri("headerType=http&host=front.invalid&path=%2Fapi"))
        h=c["streamSettings"]["rawSettings"]["header"]
        self.assertEqual(h["type"],"http");self.assertEqual(h["request"]["path"],["/api"])
        self.assertEqual(h["request"]["headers"]["Host"],["front.invalid"])
        self.reject_uri(self.trojan_uri("type=ws&headerType=http"))
    def test_audit_vmess_unsafe_and_extra_rejected(self):
        for kw in [dict(insecure="1"),dict(allowInsecure=True),dict(allow_insecure=True),
                   dict(allowInsecure=False,insecure=1),dict(net="xhttp",extra="{broken"),
                   dict(net="xhttp",extra=[]),dict(net="tcp",extra={"noSSEHeader":True})]:
            self.reject_uri(self.audit_vmess(**kw))
        n,c=self.one(self.audit_vmess(net="xhttp",extra={"noSSEHeader":True}))
        self.assertEqual(c["streamSettings"]["xhttpSettings"]["extra"],{"noSSEHeader":True})
    def test_audit_new_field_validation(self):
        for q in ["pcs=bad","pcs="+"ab"*32+"&pcs="+"cd"*32,"ech=a&ech=b", "vcn=a&vcn=b"]:
            self.reject_uri(self.vless_uri("security=tls&"+q))
        self.reject_uri(self.vless_uri("security=none&pcs="+"ab"*32))
        self.reject_uri(self.audit_vmess(pcs=["ab"*32]))
    def test_audit_new_fields_exact_dedupe(self):
        for field,a,b in [("pcs","ab"*32,"cd"*32),("vcn","a.invalid","b.invalid"),("ech","a.invalid","b.invalid")]:
            uris=[self.vless_uri("security=tls&"+urlencode({field:v})) for v in [a,b]]
            r,nodes=self.extract(b"\n".join(uris));self.assertEqual(r["accepted"],2,r)
            for n in nodes:self.generate(n)
    def test_audit_no_parameter_leak_between_imports(self):
        first=self.vless_uri("security=tls&pcs="+"ab"*32).decode();second=self.trojan_uri().decode()
        r=self.shell('. "$BRORAY_ROOT/lib/server-import.sh"; broray_server_import_dispatch "$1" subscription fixture 1; broray_server_import_dispatch "$2" subscription fixture 2',first,second)
        self.assertEqual(r.returncode,0,r.stderr)
        n=json.loads((self.app/"servers/subscription-fixture-0002.json").read_bytes())
        self.assertNotIn("pinnedPeerCertSha256",n["tls"]);self.generate(n)
    def test_audit_json_stream_fields(self):
        p=profile("raw","tls");fm={"tcp":[{"type":"fragment","settings":{"packets":"tlshello","length":"100-200","delay":"10-20"}}]}
        stream(p)["tlsSettings"].update(echConfigList="cloudflare-ech.com",pinnedPeerCertSha256="ab"*32,verifyPeerCertByName="cert.invalid")
        stream(p)["finalmask"]=fm
        n,c=self.one(p)
        self.assertEqual(c["streamSettings"]["finalmask"],fm)
        for key in ["echConfigList","pinnedPeerCertSha256","verifyPeerCertByName"]:
            self.assertEqual(c["streamSettings"]["tlsSettings"][key],stream(p)["tlsSettings"][key])
        p=profile("grpc","reality");stream(p)["realitySettings"]["mldsa65Verify"]="A"*2603
        n,c=self.one(p);self.assertEqual(c["streamSettings"]["realitySettings"]["mldsa65Verify"],"A"*2603)
    def test_audit_vmess_modes_preserved(self):
        for mode in ["gun","multi"]:
            n,c=self.one(self.audit_vmess(net="grpc",type=mode,path="api",host="front.invalid"))
            self.assertEqual(c["streamSettings"]["grpcSettings"].get("multiMode"),mode=="multi")
            self.assertEqual(c["streamSettings"]["grpcSettings"]["serviceName"],"api")
        self.reject_uri(self.audit_vmess(net="grpc",type="multi",mode="gun"))
    def test_audit_failed_parse_after_valid_not_saved(self):
        first=self.trojan_uri("pcs="+"ab"*32).decode()
        second=self.trojan_uri("pcs=a&pcs=b").decode()
        r=self.shell('. "$BRORAY_ROOT/lib/server-import.sh"; broray_server_import_dispatch "$1" subscription fixture 1; broray_server_import_dispatch "$2" subscription fixture 2',first,second)
        self.assertNotEqual(r.returncode,0)
        self.assertEqual(len(list((self.app/"servers").glob("*.json"))),1)
    def test_audit_vmess_xhttp_type_mode(self):
        # v2rayN exports XhttpMode in the legacy JSON field "type".
        n,c=self.one(self.audit_vmess(net="xhttp",type="stream-one",path="/api"))
        self.assertEqual(n["transport"]["mode"],"stream-one")
        self.assertEqual(c["streamSettings"]["xhttpSettings"]["mode"],"stream-one")
        self.reject_uri(self.audit_vmess(net="xhttp",type="stream-one",mode="packet-up"))
        self.reject_uri(self.audit_vmess(net="xhttp",type="invalid"))
    def test_expansion_modern_vmess(self):
        for network,security in [("raw","none"),("raw","tls"),("grpc","tls"),("ws","tls"),("httpupgrade","tls"),("xhttp","tls"),("raw","reality")]:
            q=dict(type=network,security=security,encryption="auto",sni="front.invalid")
            if security=="reality":q["pbk"]="A"*43
            n,c=self.one(("vmess://11111111-2222-4333-8444-555555555555@vpn.example.invalid:443/?"+urlencode(q)).encode())
            self.assertEqual(n["protocol"],"vmess");self.assertEqual(n["alterId"],0)
            self.assertEqual(c["settings"]["vnext"][0]["users"][0]["security"],"auto")
        for query in ["encryption=bad","encryption=auto&encryption=none","flow=xtls-rprx-vision","aid=1","allowInsecure=true"]:
            self.reject_uri(("vmess://11111111-2222-4333-8444-555555555555@vpn.example.invalid:443?"+query).encode())
    def test_expansion_vision_udp443(self):
        for security in ["tls","reality"]:
            p=profile("raw",security);user(p)["flow"]="xtls-rprx-vision-udp443"
            q=dict(security=security,flow="xtls-rprx-vision-udp443",sni="front.invalid")
            if security=="reality":q["pbk"]="A"*43
            for payload in [p,self.vless_uri(urlencode(q))]:
                n,c=self.one(payload);self.assertEqual(c["settings"]["vnext"][0]["users"][0]["flow"],q["flow"])
        self.reject_uri(self.vless_uri("type=ws&security=tls&flow=xtls-rprx-vision-udp443"))
    def test_expansion_mkcp(self):
        for proto in ["vless","vmess"]:
            q=dict(type="kcp",security="none",mtu="1350",tti="20",seed="secret+seed",headerType="srtp")
            n,c=self.one((proto+"://11111111-2222-4333-8444-555555555555@vpn.example.invalid:443?"+urlencode(q)).encode())
            st=c["streamSettings"];self.assertEqual(st["network"],"kcp")
            self.assertEqual(st["kcpSettings"],{"mtu":1350,"tti":20})
            # Xray FinalMask applies the first mask as the innermost layer.
            self.assertEqual(st["finalmask"]["udp"],[{"type":"mkcp-legacy","settings":{"value":"secret+seed"}},{"type":"mkcp-legacy","settings":{"header":"srtp"}}])
        for q in ["type=kcp&mtu=bad","type=kcp&mtu=20","type=kcp&tti=9","type=kcp&tti=1001","type=kcp&security=reality&pbk="+"A"*43,"type=kcp&headerType=bogus"]:
            self.reject_uri(self.vless_uri(q))
    def test_expansion_mkcp_legacy_vmess(self):
        n,c=self.one(self.audit_vmess(net="kcp",tls="",type="wechat-video",path="legacy-seed"))
        self.assertEqual(c["streamSettings"]["finalmask"]["udp"],[{"type":"mkcp-legacy","settings":{"value":"legacy-seed"}},{"type":"mkcp-legacy","settings":{"header":"wechat"}}])
    def test_expansion_trojan_reality(self):
        for network in ["raw","grpc","xhttp"]:
            n,c=self.one(self.trojan_uri(urlencode(dict(type=network,security="reality",sni="front.invalid",pbk="A"*43))))
            self.assertEqual(c["streamSettings"]["realitySettings"]["publicKey"],"A"*43)
        self.reject_uri(self.trojan_uri("security=reality&type=ws&sni=front.invalid&pbk="+"A"*43))
    def test_expansion_mkcp_json_and_masks(self):
        p=profile("raw","none");stream(p).clear()
        stream(p).update(network="kcp",security="none",kcpSettings={"mtu":1400,"seed":"seed","header":{"type":"srtp"}})
        n,c=self.one(p)
        self.assertEqual(c["streamSettings"]["kcpSettings"],{"mtu":1400})
        self.assertEqual(c["streamSettings"]["finalmask"]["udp"],[{"type":"mkcp-legacy","settings":{"value":"seed"}},{"type":"mkcp-legacy","settings":{"header":"srtp"}}])
        for header in ["none","utp","dtls","wireguard","dns"]:
            q=dict(type="kcp",security="none",headerType=header)
            if header=="dns":q["host"]="mask.invalid"
            n,c=self.one(self.vless_uri(urlencode(q)))
            masks=c["streamSettings"]["finalmask"]["udp"]
            self.assertEqual(masks[0],{"type":"mkcp-legacy","settings":{}})
            if header!="none":self.assertEqual(masks[1]["settings"]["header"],header)
        n,c=self.one(self.trojan_uri("type=kcp&security=tls&seed=secret&headerType=none"))
        self.assertEqual(c["streamSettings"]["network"],"kcp")
        self.assertEqual(c["streamSettings"]["security"],"tls")
    def test_expansion_mkcp_identity(self):
        uris=[self.vless_uri("type=kcp&security=none&seed="+seed) for seed in ["one","two"]]
        result,nodes=self.extract(b"\n".join(uris))
        self.assertEqual(result["accepted"],2,result)
        for n in nodes:self.generate(n)
        self.reject_uri(self.audit_vmess(net="kcp",tls="",path="one",seed="two"))
        for key in ["congestion","readBufferSize","writeBufferSize"]:
            self.reject_uri(self.vless_uri("type=kcp&"+key+"=1"))
            self.reject_uri(self.audit_vmess(net="kcp",tls="",**{key:1}))
    def test_expansion_preserves_existing_grpc_default(self):
        n,c=self.one(self.vless_uri("security=tls&type=grpc&serviceName=api"))
        self.assertEqual(n["transport"]["mode"],"auto")
        self.assertEqual(n["xhttp"]["mode"],"auto")
        self.assertFalse(c["streamSettings"]["grpcSettings"]["multiMode"])
    def test_expansion_modern_vmess_cipher_and_fragment(self):
        prefix="vmess://11111111-2222-4333-8444-555555555555@vpn.example.invalid:443?"
        for cipher in ["auto","aes-128-gcm","chacha20-poly1305","none"]:
            n,c=self.one((prefix+"security=tls&encryption="+cipher).encode())
            self.assertEqual(c["settings"]["vnext"][0]["users"][0]["security"],cipher)
        self.one(self.audit_vmess()+b"#name@domain")
    def test_expansion_parameter_loss_rejected(self):
        for q in ["type=raw&seed=secret","type=ws&headerType=http","type=xhttp&mode=invalid",
                  "type=grpc&mode=invalid","type=raw&extra=%7B%22noSSEHeader%22%3Atrue%7D",
                  "security=tls&insecure=true"]:
            self.reject_uri(self.vless_uri(q))
            self.reject_uri(self.vless_uri(q).replace(b"vless://",b"vmess://",1))
    def test_expansion_vless_encryption_padding(self):
        for padding in ["100-100-200", "100-100-200.50-0-10.50-0-20"]:
            encryption="mlkem768x25519plus.native.0rtt."+padding+"."+"A"*43
            p=profile("raw","tls");user(p)["encryption"]=encryption
            for payload in [p,self.vless_uri("security=tls&encryption="+encryption)]:
                n,c=self.one(payload)
                self.assertEqual(c["settings"]["vnext"][0]["users"][0]["encryption"],encryption)
        for suffix in ["100-1-20."+"A"*43,"100-200-100."+"A"*43,"bad."+"A"*43,"A"*43+".100-100-200"]:
            self.reject_uri(self.vless_uri("security=tls&encryption=mlkem768x25519plus.native.0rtt."+suffix))
    def test_tcp_tls(self):
        n,c=self.one(profile());self.assertEqual(c['streamSettings']['network'],'raw')
        self.assertEqual(c['streamSettings']['tlsSettings']['serverName'],'sni.example.invalid')
    def test_duplicate_uri_parameters_never_saved(self):
        for key in ['type','security','flow','fp','allowInsecure','serviceName']:
            with self.subTest(key=key):
                result,nodes=self.extract(('vless://11111111-2222-4333-8444-555555555555@vpn.example.invalid:443?'+key+'=&'+key+'=').encode())
                self.assertEqual(nodes,[],result)
                self.assertEqual(result['accepted'],0,result)
    def test_duplicate_after_previous_parse_cannot_reuse_values(self):
        valid=self.vless_uri().decode()
        invalid=self.vless_uri('security=tls&allowInsecure=&allowInsecure=').decode()
        r=self.shell('. "$BRORAY_ROOT/lib/server-import.sh"; broray_server_import_dispatch "$1" subscription fixture 1; broray_server_import_dispatch "$2" subscription fixture 2',valid,invalid)
        self.assertNotEqual(r.returncode,0)
        rows=list((self.app/'servers').glob('*.json'))
        self.assertEqual(len(rows),1)
        self.generate(json.loads(rows[0].read_bytes()))
    def test_trojan_encoded_extra_key_rejects_wrong_transport(self):
        for value in ['', '%7B%7D']:
            self.reject_uri(self.trojan_uri('type=raw&%65xtra='+value))
        n,c=self.one(self.trojan_uri('type=xhttp&%65xtra='+quote('{"noSSEHeader":true}')))
        self.assertEqual(c['streamSettings']['xhttpSettings']['extra'],{'noSSEHeader':True})
    def test_tls_connection_key_matches_generated_settings(self):
        n,c=self.one(profile())
        for field,value in [('serverName','changed.invalid'),('fingerprint','chrome')]:
            other=copy.deepcopy(n);other['tls'][field]=value
            c2=self.generate(other)
            self.assertNotEqual(c['streamSettings']['tlsSettings'][field],c2['streamSettings']['tlsSettings'][field])
            (self.app/'key-a.json').write_text(json.dumps(n));(self.app/'key-b.json').write_text(json.dumps(other))
            r=self.shell('. "$BRORAY_ROOT/lib/server-subscription-service.sh"; broray_server_subscription_import_key "$BRORAY_ROOT/key-a.json"; broray_server_subscription_import_key "$BRORAY_ROOT/key-b.json"')
            self.assertEqual(r.returncode,0,r.stderr)
            keys=r.stdout.splitlines();self.assertEqual(len(keys),2);self.assertNotEqual(keys[0],keys[1])
            r=self.shell('. "$BRORAY_ROOT/lib/server-subscription-service.sh"; broray_server_subscription_continuity_key "$BRORAY_ROOT/key-a.json"; broray_server_subscription_continuity_key "$BRORAY_ROOT/key-b.json"')
            self.assertEqual(r.returncode,0,r.stderr);keys=r.stdout.splitlines();self.assertEqual(len(keys),2)
            self.assertEqual(keys[0]==keys[1],field=='fingerprint')
    def vless_uri(self, query='security=tls', identity='11111111-2222-4333-8444-555555555555', suffix=''):
        return f'vless://{identity}@vpn.example.invalid:443{suffix}?{query}'.encode()
    def reject_uri(self, uri):
        result,nodes=self.extract(uri)
        self.assertEqual(result['accepted'],0,result)
        self.assertEqual(nodes,[],result)
    def test_vless_encoded_id(self):
        n,c=self.one(self.vless_uri(identity='%31'+'11111111-2222-4333-8444-555555555555'[1:]))
        self.assertEqual(n['uuid'],'11111111-2222-4333-8444-555555555555')
        self.assertEqual(c['settings']['vnext'][0]['users'][0]['id'],n['uuid'])
        self.reject_uri(self.vless_uri(identity='%GG'))
    def test_vless_root_slash(self):
        n,c=self.one(self.vless_uri(suffix='/'))
        self.assertEqual(n['port'],443)
        n,c=self.one(b'vless://11111111-2222-4333-8444-555555555555@vpn.example.invalid:443/')
        self.assertEqual(n['port'],443)
        self.reject_uri(self.vless_uri(suffix='/unexpected'))
    def test_vless_grpc_authority(self):
        for extra in ['authority=front.invalid','host=front.invalid','authority=front.invalid&host=front.invalid']:
            with self.subTest(extra=extra):
                n,c=self.one(self.vless_uri('security=tls&type=grpc&serviceName=api&'+extra))
                self.assertEqual(n['transport']['host'],'front.invalid')
                self.assertEqual(c['streamSettings']['grpcSettings']['authority'],'front.invalid')
        self.reject_uri(self.vless_uri('security=tls&type=grpc&host=a.invalid&authority=b.invalid'))
    def test_vless_encrypted_vision_xhttp_uri_and_json(self):
        encryption='mlkem768x25519plus.random.0rtt.'+'A'*43
        payload=profile('xhttp','reality')
        user(payload).update(encryption=encryption,flow='xtls-rprx-vision')
        query=urlencode(dict(type='xhttp',security='reality',sni='front.invalid',pbk='A'*43,
                            encryption=encryption,flow='xtls-rprx-vision'))
        for value in [payload,self.vless_uri(query)]:
            n,c=self.one(value)
            self.assertEqual(n['encryption'],encryption)
            self.assertEqual(n['flow'],'xtls-rprx-vision')
            self.assertEqual(c['settings']['vnext'][0]['users'][0]['encryption'],encryption)
            self.assertEqual(c['settings']['vnext'][0]['users'][0]['flow'],'xtls-rprx-vision')
    def test_vless_invalid_encryption_rejected_uri_and_json(self):
        for encryption in ['garbage','mlkem768x25519plus.random.0rtt.bad',
                           'mlkem768x25519plus.other.0rtt.'+'A'*43,
                           'mlkem768x25519plus.random.9rtt.'+'A'*43]:
            self.reject_uri(self.vless_uri(urlencode(dict(encryption=encryption))))
            payload=profile('xhttp','reality');user(payload)['encryption']=encryption
            result,nodes=self.extract(payload)
            self.assertEqual(result['accepted'],0,result);self.assertEqual(nodes,[])
    def test_vless_security_matrix(self):
        for network,security,flow in [('raw','tls',''),('raw','reality',''),
              ('raw','tls','xtls-rprx-vision'),('raw','reality','xtls-rprx-vision'),
              ('grpc','tls',''),('grpc','reality',''),('xhttp','tls',''),('xhttp','reality',''),
              ('ws','tls',''),('httpupgrade','tls','')]:
            with self.subTest(network=network,security=security,flow=flow):
                query={'type':network,'security':security,'flow':flow,'sni':'front.invalid'}
                if security=='reality':query['pbk']='A'*43
                if network=='grpc':query['authority']='front.invalid'
                n,c=self.one(self.vless_uri(urlencode(query)))
                self.assertEqual(n['flow'],flow or None)
                self.assertEqual(c['streamSettings']['security'],security)
                self.assertNotIn('allowInsecure',c['streamSettings'].get('tlsSettings',{}))
        for network in ['ws','httpupgrade']:
            self.reject_uri(self.vless_uri(f'type={network}&security=reality&sni=front.invalid&pbk='+('A'*43)))
        for network in ['grpc','ws','httpupgrade','xhttp']:
            self.reject_uri(self.vless_uri(f'type={network}&security=tls&flow=xtls-rprx-vision'))
    def test_vless_tls_vision_json(self):
        p=profile('raw','tls');user(p)['flow']='xtls-rprx-vision'
        n,c=self.one(p)
        self.assertEqual(c['settings']['vnext'][0]['users'][0]['flow'],'xtls-rprx-vision')
    def test_vless_allow_insecure_explicit(self):
        for value in ['true','1','yes']:
            self.reject_uri(self.vless_uri('security=tls&allowInsecure='+value))
        for value in ['false','0','']:
            n,c=self.one(self.vless_uri('security=tls&allowInsecure='+value))
            self.assertFalse(n['tls']['allowInsecure'])
            self.assertNotIn('allowInsecure',c['streamSettings']['tlsSettings'])
    def trojan_uri(self,query='',password='password'):
        return f'trojan://{password}@vpn.example.invalid:443?{query}'.encode()
    def test_trojan_password_components(self):
        for password,expected in [('a+b','a+b'),('a%2Bb','a+b'),('a%252Bb','a%2Bb')]:
            n,c=self.one(self.trojan_uri(password=password))
            self.assertEqual(n['password'],expected)
            self.assertEqual(c['settings']['servers'][0]['password'],expected)
    def test_trojan_transport_modes(self):
        for network,mode in [('raw',''),('grpc','gun'),('xhttp','auto'),('xhttp','packet-up'),
                             ('xhttp','stream-up'),('xhttp','stream-one'),('grpc','multi')]:
            query='type='+network
            if mode not in ['','gun','auto']:query+='&mode='+mode
            n,c=self.one(self.trojan_uri(query))
            self.assertEqual(n['transport']['mode'],mode)
            if network=='xhttp':self.assertEqual(c['streamSettings']['xhttpSettings']['mode'],mode)
        for query in ['type=xhttp&mode=gun','type=grpc&mode=auto']:
            self.reject_uri(self.trojan_uri(query))
    def test_trojan_extra_preserved(self):
        extra={'noSSEHeader':True,'headers':{'X-Test':'a+b%20'},'xmux':{'maxConnections':2}}
        n,c=self.one(self.trojan_uri('type=xhttp&extra='+quote(json.dumps(extra))))
        self.assertEqual(n['transport']['extra'],extra)
        self.assertEqual(c['streamSettings']['xhttpSettings']['extra'],extra)
        n,c=self.one(self.trojan_uri('type=xhttp&extra='))
        self.assertEqual(n['transport']['extra'],{})
        for query in ['type=xhttp&extra=%7Bbroken','type=xhttp&extra=%5B%5D','type=grpc&extra=%7B%7D','type=raw&extra=']:
            self.reject_uri(self.trojan_uri(query))
    def test_trojan_insecure_explicit(self):
        self.reject_uri(self.trojan_uri('allowInsecure=true'))
        for value in ['false','']:
            n,c=self.one(self.trojan_uri('allowInsecure='+value))
            self.assertFalse(n['tls']['allowInsecure'])
            self.assertNotIn('allowInsecure',c['streamSettings']['tlsSettings'])
    def test_subscription_bom_four_inputs(self):
        for raw in [self.vless_uri(),json.dumps(profile()).encode()]:
            for encoded in [False,True]:
                with self.subTest(json=raw.startswith(b'{'),base64=encoded):
                    reference=base64.b64encode(raw) if encoded else raw
                    n,c=self.one(reference)
                    with_bom=b'\xef\xbb\xbf'+raw
                    n2,c2=self.one(base64.b64encode(with_bom) if encoded else with_bom)
                    self.assertEqual(c2,c)
                    self.assertEqual(n2['uuid'],n['uuid'])
                    self.assertEqual(n2['source']['importKey'],n['source']['importKey'])
    def test_bom_removed_only_at_start(self):
        for raw,expected in [(b'\xef\xbb\xbfabc',b'abc'),(b'abc\xef\xbb\xbf',b'abc\xef\xbb\xbf'),
                             (b' \xef\xbb\xbfabc',b' \xef\xbb\xbfabc'),(b'\xef\xbb',b'\xef\xbb')]:
            (self.app/'bom-in').write_bytes(raw)
            p=self.shell('. "$BRORAY_ROOT/lib/subscription-service.sh"; broray_subscription_strip_bom "$BRORAY_ROOT/bom-in" "$BRORAY_ROOT/bom-out"')
            self.assertEqual(p.returncode,0,p.stderr)
            self.assertEqual((self.app/'bom-out').read_bytes(),expected)
    def test_uri_list_outer_whitespace_only(self):
        uri=self.vless_uri('security=tls&type=ws&path=%2Fa%20b%2B%2520')
        n,c=self.one(uri)
        for raw in [b'  \t'+uri+b' \t\n',base64.b64encode(b'  '+uri+b'  \n')]:
            n2,c2=self.one(raw)
            self.assertEqual(n2['uri'],n['uri'])
            self.assertEqual(c2,c)
            self.assertEqual(n2['transport']['path'],'/a b+%20')
    def test_exact_subscription_duplicate(self):
        uri=self.vless_uri()
        r,nodes=self.extract(uri+b'\n'+uri)
        self.assertEqual((r['accepted'],r['rejected']),(1,1),r)
        self.generate(nodes[0])
    def test_distinct_connection_parameters_not_deduplicated(self):
        base='type=ws&security=tls&host=a.invalid&path=%2Fa'
        variants=[(self.vless_uri(base),self.vless_uri(base.replace('host=a.invalid','host=b.invalid'))),
                  (self.vless_uri(),self.vless_uri(identity='22222222-2222-4333-8444-555555555555')),
                  (self.vless_uri(base),self.vless_uri(base.replace('path=%2Fa','path=%2Fb'))),
                  (self.vless_uri('security=reality&sni=a.invalid&pbk='+'A'*43),
                   self.vless_uri('security=reality&sni=a.invalid&pbk='+'B'*42+'A'))]
        for first,second in variants:
            with self.subTest(first=first,second=second):
                r,nodes=self.extract(first+b'\n'+second)
                self.assertEqual((r['accepted'],r['rejected']),(2,0),r)
                self.assertEqual(len({n['source']['importKey'] for n in nodes}),2)
                for n in nodes:self.generate(n)
    def test_grpc_tls(self):
        n,c=self.one(profile('grpc'));g=c['streamSettings']['grpcSettings']
        self.assertEqual(g,{'serviceName':'api/a+b%20?c&d','authority':'front.example.invalid','multiMode':True})
    def test_ws_tls_unicode_percent(self):
        n,c=self.one(profile('ws'));self.assertEqual(n['name'],'Тест + # % / 東京')
        self.assertEqual(c['streamSettings']['wsSettings'],{'host':'front.example.invalid','path':'/a+b%20?q=x&n=1'})
    def test_ws_legacy_host_header(self):
        p=profile('ws');w=stream(p)['wsSettings'];w['headers']={'Host':w.pop('host')}
        _,c=self.one(p);self.assertEqual(c['streamSettings']['wsSettings']['host'],'front.example.invalid')
    def test_httpupgrade_tls(self):
        _,c=self.one(profile('httpupgrade'));self.assertEqual(c['streamSettings']['httpupgradeSettings'],{'host':'front.example.invalid','path':'/up?q=1'})
    def test_xhttp_tls_host_and_tuning(self):
        p=profile('xhttp');stream(p)['xhttpSettings']['extra']={'noSSEHeader':True,'xPaddingBytes':'100-200','xmux':{'maxConnections':'2-3'}}
        n,c=self.one(p);x=c['streamSettings']['xhttpSettings']
        self.assertEqual(x['host'],'front.example.invalid');self.assertEqual(x['extra'],stream(p)['xhttpSettings']['extra'])
    def test_xhttp_direct_tuning_preserved(self):
        p=profile('xhttp');stream(p)['xhttpSettings'].update({'noSSEHeader':True,'scMinPostsIntervalMs':10})
        _,c=self.one(p);self.assertEqual(c['streamSettings']['xhttpSettings']['extra'],{'noSSEHeader':True,'scMinPostsIntervalMs':10})
    def test_reality_password_tcp_vision(self):
        p=profile('tcp','reality');r=stream(p)['realitySettings'];r['password']=r.pop('publicKey');user(p)['flow']='xtls-rprx-vision'
        n,c=self.one(p);self.assertEqual(c['streamSettings']['realitySettings']['publicKey'],r['password'])
        self.assertEqual(c['settings']['vnext'][0]['users'][0]['flow'],'xtls-rprx-vision')
    def test_reality_grpc(self):
        n,c=self.one(profile('grpc','reality'));self.assertEqual(c['streamSettings']['realitySettings']['shortId'],'01234567')
    def test_reality_xhttp(self):
        _,c=self.one(profile('xhttp','reality'));self.assertEqual(c['streamSettings']['xhttpSettings']['host'],'front.example.invalid')
    def test_flat_settings(self):
        _,c=self.one(profile('ws',flat=True));self.assertEqual(c['settings']['vnext'][0]['address'],'vpn.example.invalid')
    def test_ipv6(self):
        _,c=self.one(profile(address='2001:db8::1'));self.assertEqual(c['settings']['vnext'][0]['address'],'[2001:db8::1]')
    def test_ipv6_mapped_tail(self):
        _,c=self.one(profile(address='::ffff:192.0.2.1'));self.assertEqual(c['settings']['vnext'][0]['address'],'[::ffff:192.0.2.1]')
    def test_base64_json_container(self):
        payload=base64.urlsafe_b64encode(json.dumps([profile('ws')],ensure_ascii=False).encode()).rstrip(b'=')
        _,c=self.one(payload);self.assertEqual(c['streamSettings']['network'],'ws')
    def test_aliases_array(self):
        payload=[profile('websocket'),profile('httpUpgrade'),profile('splithttp')]
        result,nodes=self.extract(payload);self.assertEqual(result['accepted'],3,result)
        self.assertEqual({n['network'] for n in nodes},{'ws','httpupgrade','xhttp'})
        for n in nodes: self.generate(n)
    def test_json_uri_equivalence_without_id_migration(self):
        p=profile('xhttp');n,c=self.one(p)
        uri=n['uri'];result,nodes=self.extract(uri.encode())
        self.assertEqual(result['rc'],0,result);self.assertEqual(len(nodes),1)
        # Repeat staging of the same endpoint must retain all model values.
        fields=['id','name','uri','protocol','address','port','uuid','encryption','flow','network','security','tls','reality','transport','xhttp']
        # Staging legitimately refreshes source observation timestamps.
        self.assertEqual({k:nodes[0][k] for k in fields},{k:n[k] for k in fields})
        self.assertEqual(nodes[0]['source']['subscriptionId'],n['source']['subscriptionId'])
        self.assertEqual(self.generate(nodes[0]),c)
    def test_foreign_files_untouched_by_failed_preparation(self):
        manual=self.app/'servers/manual-qa.json';other=self.app/'servers/other-subscription-qa.json'
        manual.write_bytes(b'{"qa":"manual"}');other.write_bytes(b'{"qa":"other"}')
        before={p.name:p.read_bytes() for p in (self.app/'servers').iterdir()}
        result,nodes=self.extract({'servers':[]});self.assertNotEqual(result['rc'],0)
        self.assertEqual(nodes,[]);self.assertEqual(before,{p.name:p.read_bytes() for p in (self.app/'servers').iterdir()})
    def test_partial_outbound_is_not_staged(self):
        p=profile();bad=copy.deepcopy(user(p));bad['encryption']='PRIVATE_CANARY'
        outbound(p)['settings']['vnext'][0]['users'].append(bad)
        result,nodes=self.extract(p);self.assertEqual(result['errorCode'],'NO_VALID_NODES')
        self.assertEqual(nodes,[]);self.assertEqual(result['accepted'],0)
    def test_existing_balancer_extraction_contract_is_not_execution(self):
        p=profile('ws');p['routing']={'balancers':[{'tag':'b','selector':['proxy']}]}
        p['dns']={'servers':['https://dns.example.invalid/dns-query']};p['inbounds']=[{'port':9999}]
        _,c=self.one(p);self.assertEqual(c['streamSettings']['network'],'ws')
        # Main generator guard above verifies top-level routing/DNS not copied.
    def test_generator_without_xhttp_host_is_unchanged(self):
        p=profile('xhttp');stream(p)['xhttpSettings'].pop('host')
        _,c=self.one(p);self.assertNotIn('host',c['streamSettings']['xhttpSettings'])

if __name__=='__main__':
    if os.name=='nt': raise SystemExit('Run in disposable Linux guest only')
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Pipeline))
    report={'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,
        'coreValidationRequested':bool(XRAY),'coreValidations':len(CORE_RESULTS),'coreResults':CORE_RESULTS,
        'routerAccessed':False,'providerAccessed':False,'networkRequested':False,
        'scope':'Actual extractor/stager/parser/generator, no HTTP transport or protected catalog commit'}
    print('STAGE05_PIPELINE_REPORT='+json.dumps(report),flush=True)
    raise SystemExit(0 if result.wasSuccessful() else 1)
