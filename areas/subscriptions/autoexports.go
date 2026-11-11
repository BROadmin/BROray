package subscriptions

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/strictjson"
)

const maxDiscoveryBytes = 512 * 1024
const maxCombinedBytes = 4 * 1024 * 1024
const maxHTMLTokens = 4096

type exportLink struct{ format, url string }
type sourceDocument struct {
	body   []byte
	format string
}

func exportError() *subErr {
	return fail("ALTERNATE_EXPORT_ERROR", "Не удалось полностью получить дополнительные форматы подписки. Прежние серверы сохранены.")
}

func exportFailure(format string) *subErr {
	label := map[string]string{"json": "JSON", "clash": "Clash"}[format]
	if label == "" {
		return exportError()
	}
	return fail("ALTERNATE_EXPORT_ERROR", "Не удалось полностью получить формат "+label+" подписки. Прежние серверы сохранены.")
}

func normalizedOrigin(p parsedURL) string {
	port, _ := strconv.Atoi(p.Port)
	return p.Scheme + ":" + strings.TrimSuffix(p.Host, ".") + ":" + strconv.Itoa(port)
}

func htmlPayload(body []byte) bool {
	b := strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff"))
	return strings.HasPrefix(b, "<")
}

// exportURL accepts only explicit same-origin references. URL fragments do
// not identify a different HTTP document. DNS/IP/TLS checks still run in fetch.
func exportURL(base, ref string) (string, *subErr) {
	if ref == "" || len(ref) > 4096 || strings.ContainsAny(ref, "\\\r\n\t\x00") {
		return "", exportError()
	}
	b, err := url.Parse(base)
	if err != nil {
		return "", exportError()
	}
	r, err := url.Parse(ref)
	if err != nil || r.User != nil || r.Opaque != "" {
		return "", exportError()
	}
	u := b.ResolveReference(r)
	u.Fragment = ""
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	p, pe := parseURL(u.String())
	bp, be := parseURL(base)
	if pe != nil || be != nil || normalizedOrigin(p) != normalizedOrigin(bp) {
		return "", exportError()
	}
	// Normalize the effective port so equivalent advertised URLs deduplicate.
	port, _ := strconv.Atoi(p.Port)
	host := strings.TrimSuffix(p.Host, ".")
	if port == 80 && p.Scheme == "http" || port == 443 && p.Scheme == "https" {
		u.Host = host
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	} else {
		u.Host = net.JoinHostPort(host, strconv.Itoa(port))
	}
	return u.String(), nil
}

var dataAssignment = regexp.MustCompile(`(?:^|;)\s*window\.__SUB_PAGE_DATA__\s*=`)
var htmlAttribute = regexp.MustCompile("^([^\\s=/>]+)(?:\\s*=\\s*(?:\"([^\"]*)\"|'([^']*)'|([^\\s\"'=<>`]+)))?")
var formatWord = regexp.MustCompile(`(?i)\b(json|clash)\b`)

// discoverExports is a bounded static HTML reader, not a JavaScript engine.
// It recognizes only the documented data assignment and explicit anchors.
// Script/style bodies are raw text, so apparent anchors inside them are inert.
func discoverExports(ctx context.Context, body []byte, base string) ([]exportLink, *subErr) {
	if len(body) > maxDiscoveryBytes {
		return nil, errTooLarge
	}
	text := string(body)
	// HTML delimiters are ASCII. Keep byte offsets aligned with text even
	// when Unicode case folding would shorten or expand a UTF-8 sequence.
	lower := asciiLower(text)
	links := []exportLink{}
	byFormat := map[string]string{}
	byURL := map[string]bool{}
	add := func(format, ref string) *subErr {
		u, err := exportURL(base, ref)
		if err != nil {
			return err
		}
		if old := byFormat[format]; old != "" && old != u {
			return exportError()
		}
		byFormat[format] = u
		if byURL[u] {
			for _, link := range links {
				if link.url == u && link.format != format {
					return exportError()
				}
			}
		} else {
			links = append(links, exportLink{format, u})
			byURL[u] = true
		}
		return nil
	}
	var anchor map[string]string
	var label strings.Builder
	assignments := 0
	for pos, tokens := 0, 0; pos < len(text); tokens++ {
		if ctx.Err() != nil {
			return nil, exportError()
		}
		if tokens >= maxHTMLTokens {
			return nil, errTooLarge
		}
		next := strings.IndexByte(text[pos:], '<')
		if next < 0 {
			next = len(text) - pos
		}
		if anchor != nil {
			label.WriteString(text[pos : pos+next])
		}
		pos += next
		if pos == len(text) {
			break
		}
		if strings.HasPrefix(text[pos:], "<!--") {
			end := strings.Index(text[pos+4:], "-->")
			if end < 0 {
				return nil, exportError()
			}
			pos += end + 7
			continue
		}
		start, quote := pos+1, byte(0)
		pos++
		for ; pos < len(text); pos++ {
			c := text[pos]
			if quote != 0 {
				if c == quote {
					quote = 0
				}
				continue
			}
			if c == '\'' || c == '"' {
				quote = c
				continue
			}
			if c == '>' {
				break
			}
		}
		if pos == len(text) {
			return nil, exportError()
		}
		tag := strings.TrimSpace(text[start:pos])
		pos++
		closing := strings.HasPrefix(tag, "/")
		tag = strings.TrimSpace(strings.TrimPrefix(tag, "/"))
		end := strings.IndexAny(tag, " \t\r\n/>")
		if end < 0 {
			end = len(tag)
		}
		name := strings.ToLower(tag[:end])
		if name == "script" || name == "style" {
			if closing {
				continue
			}
			e := strings.Index(lower[pos:], "</"+name)
			if e < 0 {
				return nil, exportError()
			}
			if name == "script" {
				script := text[pos : pos+e]
				if strings.Contains(script, "window.__SUB_PAGE_DATA__") {
					matches := dataAssignment.FindAllStringIndex(script, 2)
					assignments += len(matches)
					if len(matches) != 1 || assignments != 1 {
						return nil, exportError()
					}
					d := json.NewDecoder(strings.NewReader(script[matches[0][1]:]))
					var raw json.RawMessage
					if d.Decode(&raw) != nil {
						return nil, exportError()
					}
					tail := strings.TrimSpace(script[matches[0][1]+int(d.InputOffset()):])
					if tail != "" && !strings.HasPrefix(tail, ";") {
						return nil, exportError()
					}
					v, err := strictjson.Decode(raw)
					m, ok := v.(map[string]any)
					if err != nil || !ok {
						return nil, exportError()
					}
					for _, field := range []struct{ key, format string }{{"subJsonUrl", "json"}, {"subClashUrl", "clash"}} {
						if v, exists := m[field.key]; exists {
							ref, ok := v.(string)
							if !ok {
								return nil, exportError()
							}
							if err := add(field.format, ref); err != nil {
								return nil, err
							}
						}
					}
				}
			}
			pos += e
			continue
		}
		if name != "a" {
			continue
		}
		if closing {
			if anchor == nil {
				continue
			}
			formats := map[string]bool{}
			for _, m := range formatWord.FindAllStringSubmatch(html.UnescapeString(label.String()+" "+anchor["title"]), -1) {
				formats[strings.ToLower(m[1])] = true
			}
			switch strings.ToLower(strings.TrimSpace(strings.Split(anchor["type"], ";")[0])) {
			case "application/json", "text/json":
				formats["json"] = true
			case "application/yaml", "text/yaml", "application/x-yaml", "text/x-yaml", "application/clash", "application/clash+yaml":
				formats["clash"] = true
			}
			if len(formats) > 1 {
				return nil, exportError()
			}
			for format := range formats {
				if err := add(format, anchor["href"]); err != nil {
					return nil, err
				}
			}
			anchor = nil
			label.Reset()
			continue
		}
		if anchor != nil {
			return nil, exportError()
		}
		anchor = map[string]string{}
		attrs := strings.TrimSpace(tag[end:])
		for attrs != "" && attrs != "/" {
			m := htmlAttribute.FindStringSubmatch(attrs)
			if m == nil {
				return nil, exportError()
			}
			key := strings.ToLower(m[1])
			if _, duplicate := anchor[key]; duplicate {
				return nil, exportError()
			}
			value := m[2] + m[3] + m[4]
			anchor[key] = html.UnescapeString(value)
			attrs = strings.TrimSpace(attrs[len(m[0]):])
		}
	}
	if anchor != nil {
		return nil, exportError()
	}
	return links, nil
}

func rememberedFormats(rec map[string]any) []string {
	var out []string
	if formats, ok := rec["alternateExports"].([]any); ok {
		for _, v := range formats {
			if v == "json" || v == "clash" {
				out = append(out, v.(string))
			}
		}
	}
	return out
}

func rememberExports(rec map[string]any, formats []string) {
	if len(formats) == 0 {
		return
	}
	all := rememberedFormats(rec)
	for _, f := range formats {
		found := false
		for _, old := range all {
			if old == f {
				found = true
			}
		}
		if !found {
			all = append(all, f)
		}
	}
	values := []any{}
	for _, f := range all {
		values = append(values, f)
	}
	rec["alternateExports"] = values
}

// fetchDocuments retains all private bodies only until staging finishes.
// Metadata from the HTML representation and exports is deliberately separate.
func (s *svc) fetchDocuments(ctx context.Context, rec map[string]any, p *prepared) error {
	in := inputOf(rec)
	final := ""
	in.FinalURL = &final
	body, err := s.fetch(ctx, in, p.md)
	if err != nil {
		return err
	}
	p.body = body
	initialOrigin, _ := parseURL(in.URL)
	finalOrigin, _ := parseURL(final)
	if normalizedOrigin(initialOrigin) != normalizedOrigin(finalOrigin) {
		// A primary redirect may be followed without carrying identity.
		// Representation negotiation must not reintroduce it on a new request.
		in.Hwid, in.SendDevice = "", false
	}
	primaryHTML := htmlPayload(body)
	page := body
	total := len(body)
	if !primaryHTML {
		p.documents = append(p.documents, sourceDocument{body: body})
		bp, _ := parseURL(final)
		probe := in
		probe.URL, probe.Accept, probe.Origin, probe.FinalURL = final, "text/html", normalizedOrigin(bp), nil
		page, err = s.fetch(ctx, probe, newMetadata())
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if len(rememberedFormats(rec)) > 0 {
				return exportError()
			}
			// A standalone URI/JSON/Clash endpoint may reject HTML negotiation.
			return nil
		}
		total += len(page)
		if total > maxCombinedBytes {
			return errTooLarge
		}
		if !htmlPayload(page) {
			page = nil
		}
	}
	var links []exportLink
	if len(page) > 0 {
		var de *subErr
		links, de = discoverExports(ctx, page, final)
		if de != nil {
			return de
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	for _, link := range links {
		p.exportFormats = append(p.exportFormats, link.format)
	}
	for _, old := range rememberedFormats(rec) {
		found := false
		for _, link := range links {
			if link.format == old {
				found = true
			}
		}
		if !found {
			return exportFailure(old)
		}
	}
	if primaryHTML && len(links) == 0 {
		return exportError()
	}
	bp, _ := parseURL(final)
	for _, link := range links {
		export := in
		export.URL, export.Origin, export.FinalURL = link.url, normalizedOrigin(bp), nil
		b, err := s.fetch(ctx, export, newMetadata())
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return exportFailure(link.format)
		}
		total += len(b)
		if total > maxCombinedBytes {
			return errTooLarge
		}
		p.documents = append(p.documents, sourceDocument{b, link.format})
	}
	return nil
}

func detectedFormat(ex *extracted) string {
	if ex.Clash {
		return "clash"
	}
	if ex.JSONImport {
		return "json"
	}
	return "uri"
}

// stageDocuments counts received/parsed occurrences across formats; accepted
// counts unique canonical configurations. An exact cross-format duplicate is
// provenance, never a rejected node. A partially invalid advertised document
// aborts the complete update before sync can touch any catalog/state.
func stageDocuments(id string, docs []sourceDocument, md *metadata, enabled bool, progress func(int, int)) (*staged, *subErr) {
	merged := &staged{}
	seen := map[string]servers.Record{}
	formats := []string{}
	for index, doc := range docs {
		dm := newMetadata()
		if index == 0 && doc.format == "" {
			dm = md
		}
		ex, err := extractNodes(doc.body, dm)
		if err != nil {
			if doc.format != "" {
				return merged, exportFailure(doc.format)
			}
			return merged, err
		}
		format := detectedFormat(ex)
		if doc.format != "" && doc.format != format {
			return merged, exportFailure(doc.format)
		}
		label := map[string]string{"uri": "URI", "json": "JSON", "clash": "Clash"}[format]
		found := false
		for _, old := range formats {
			if old == label {
				found = true
			}
		}
		if !found {
			formats = append(formats, label)
		}
		if merged.Received+len(ex.Lines) > maxNodes {
			return merged, errTooLarge
		}
		st, err := stageNodes(id, ex, enabled, progress)
		merged.Received += st.Received
		merged.Parsed += st.Parsed
		merged.Rejected += st.Rejected
		if doc.format != "" && (err != nil || st.Rejected > 0) {
			// Unknown field names are untrusted provider text too. Preserve
			// counts, but never publish node diagnostics from a failed export.
			merged.Warnings = append(merged.Warnings, "Дополнительный формат подписки содержит неподдерживаемые или некорректные узлы.")
			return merged, exportFailure(doc.format)
		}
		merged.Warnings = append(merged.Warnings, st.Warnings...)
		if err != nil {
			return merged, err
		}
		for _, r := range st.Records {
			key := sourceOf(r)["importKey"].(string)
			if old := seen[key]; old != nil {
				formats := sourceOf(old)["formats"].([]any)
				found := false
				for _, f := range formats {
					if f == format {
						found = true
					}
				}
				if !found {
					sourceOf(old)["formats"] = append(formats, format)
				}
				continue
			}
			sourceOf(r)["formats"] = []any{format}
			seen[key] = r
			merged.Records = append(merged.Records, r)
			merged.Accepted++
		}
	}
	merged.Accepted = len(merged.Records)
	if len(docs) > 1 || len(docs) == 1 && docs[0].format != "" {
		merged.Warnings = append(merged.Warnings, fmt.Sprintf("Форматы подписки: %s. Уникальных серверов: %d; точные совпадения объединены.", strings.Join(formats, ", "), merged.Accepted))
	}
	return merged, nil
}
