package keenetic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Block is one top-level configuration object with its indented body lines
// (body lines are trimmed; the terminating "!" is dropped).
type Block struct {
	Header string
	Body   []string
}

// ParseBlocks splits running-config lines into top-level blocks. Lines that
// are not indented and are not "!" start a block; indented lines belong to the
// preceding block. Top-level single lines (e.g. "ip route ...") are blocks with
// an empty body.
func ParseBlocks(lines []string) []Block {
	var blocks []Block
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" || strings.TrimSpace(line) == "!" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if len(blocks) > 0 {
				blocks[len(blocks)-1].Body = append(blocks[len(blocks)-1].Body, strings.TrimSpace(line))
			}
			continue
		}
		blocks = append(blocks, Block{Header: strings.TrimSpace(line)})
	}
	return blocks
}

// FindBlock returns the first block whose header equals header exactly.
func FindBlock(blocks []Block, header string) (Block, bool) {
	for _, b := range blocks {
		if b.Header == header {
			return b, true
		}
	}
	return Block{}, false
}

// BlockHash is a stable text form used for receipts: header + body joined.
func (b Block) Text() string {
	return b.Header + "\n" + strings.Join(b.Body, "\n")
}

// Save issues "system configuration save" and then re-reads running and
// startup configuration until two consecutive reads agree with each other,
// exactly as BROray 3.x did (up to 12 tries, 1 s apart). Returns the running
// configuration that was confirmed.
func Save(ctx context.Context, t Transport) ([]string, error) {
	res, err := t.Parse(ctx, []string{"system configuration save"})
	if err != nil {
		return nil, err
	}
	if len(res) != 1 || !res[0].OK {
		return nil, fmt.Errorf("system configuration save: %s", firstError(res))
	}
	var prevRunning, prevStartup []string
	for attempt := 0; attempt < 12; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
		}
		running, err := t.RunningConfig(ctx)
		if err != nil {
			return nil, err
		}
		startup, err := t.StartupConfig(ctx)
		if err != nil {
			return nil, err
		}
		if equalConfig(running, startup) && prevRunning != nil && equalConfig(running, prevRunning) && equalConfig(startup, prevStartup) {
			return running, nil
		}
		prevRunning, prevStartup = running, startup
	}
	return nil, errors.New("KEENETIC_SAVE_UNCONFIRMED")
}

// equalConfig compares configurations ignoring volatile leading comment and
// timestamp lines (the startup-config header differs from running-config).
func equalConfig(a, b []string) bool {
	na, nb := normalize(a), normalize(b)
	if len(na) != len(nb) {
		return false
	}
	for i := range na {
		if na[i] != nb[i] {
			return false
		}
	}
	return true
}

func normalize(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		t := strings.TrimRight(l, "\r")
		if strings.HasPrefix(t, "!") || strings.TrimSpace(t) == "" {
			continue
		}
		if strings.HasPrefix(t, "! $$$ ") || strings.HasPrefix(t, "# ") {
			continue
		}
		out = append(out, t)
	}
	return out
}

func firstError(res []Result) string {
	for _, r := range res {
		if e := r.Error(); e != "" {
			return r.Command + ": " + e
		}
		if !r.OK {
			return r.Command + ": failed"
		}
	}
	return ""
}

// Apply runs commands through the transport after allowlist validation and
// returns an error naming the first failed command. It never saves.
func Apply(ctx context.Context, t Transport, area Area, commands []string) ([]Result, error) {
	for _, c := range commands {
		if err := Allow(area, c); err != nil {
			return nil, err
		}
	}
	res, err := t.Parse(ctx, commands)
	if err != nil {
		return res, err
	}
	for _, r := range res {
		if !r.OK {
			return res, fmt.Errorf("KEENETIC_COMMAND_FAILED: %s", firstError([]Result{r}))
		}
	}
	return res, nil
}
