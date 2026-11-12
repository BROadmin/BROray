package subscriptions

import "github.com/BROadmin/broray/internal/clashimport"

// Retained for package-local compatibility; all conversion lives in the shared importer.
func clashWG(source map[string]any) string {
	input, err := clashimport.ConvertNode(source, importChecks())
	need(err == nil && input.Unavailable == nil)
	return input.Link
}
