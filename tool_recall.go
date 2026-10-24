package axon

import (
	"context"
	"encoding/json"
	"fmt"
)

// RecallTool retrieves bounded pages of historical content, without
// un-parking it or mutating the session during parallel tool execution.
func RecallTool(s *Session) Tool {
	return Tool{Name: "recall", Description: "Retrieve original historical text from a [#mN parked] breadcrumb. Historical file contents may be stale; read the current file before editing. Use offset to page through long blocks.", Schema: obj("object", props{
		"id": strSchema("Block ID, e.g. m12"), "offset": intSchema("Zero-based character offset; default 0"),
	}, []string{"id"}), Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		var args struct {
			ID     string `json:"id"`
			Offset int    `json:"offset"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", err
		}
		if args.Offset < 0 {
			return "", fmt.Errorf("offset must be nonnegative")
		}
		for _, m := range s.Messages {
			if m.ID != args.ID || m.Role == "system" {
				continue
			}
			r := []rune(m.Content)
			if args.Offset > len(r) {
				return "", fmt.Errorf("offset exceeds block length %d", len(r))
			}
			end := min(args.Offset+12000, len(r))
			return fmt.Sprintf("Historical %s (%s), characters %d:%d of %d; current files may differ:\n%s", m.ID, m.Role, args.Offset, end, len(r), string(r[args.Offset:end])), nil
		}
		return "", fmt.Errorf("block %q not found", args.ID)
	}}
}
