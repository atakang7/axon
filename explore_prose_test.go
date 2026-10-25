package axon

import (
	"context"
	"strings"
	"testing"
)

// The prose a free explorer wrote on 2026-10-26 instead of calling report,
// reduced: paths in prose, code in a fence and in inline spans, one invented.
const explorerProse = "## Rapor\n\n" +
	"Kupon `Cart.Total()` içinde uygulanıyor, **auth/token.go** dosyasında:\n\n" +
	"```go\n4\tfunc Expired(exp, now int64) bool {\n\treturn exp > now\n```\n\n" +
	"Karşılaştırma `return exp > now` satırında. Yenileme `func Refresh(t Token) error` ile yapılır.\n"

func TestProseFactsKeepOnlyQuotesFoundInNamedFiles(t *testing.T) {
	s := exploreRepo(t)
	facts, rejected := verifyFacts(s, factsFromProse(s, explorerProse))
	if len(rejected) != 0 {
		t.Fatalf("recovered candidates should all verify: %v", rejected)
	}
	var quotes []string
	for _, f := range facts {
		quotes = append(quotes, strings.TrimSpace(f.source))
		if f.path != "auth/token.go" {
			t.Fatalf("fact from a file the prose did not name: %+v", f)
		}
	}
	got := strings.Join(quotes, " | ")
	if !strings.Contains(got, "func Expired(exp, now int64) bool {") || !strings.Contains(got, "return exp > now") {
		t.Fatalf("missing real quotes: %s", got)
	}
	if strings.Contains(got, "Refresh") || strings.Contains(got, "Cart.Total") {
		t.Fatalf("a quote that is not in the file survived: %s", got)
	}
	if factsFromProse(s, "no files and no code here") != nil {
		t.Fatal("facts from prose without files")
	}
}

func TestExploreRecoversVerifiedQuotesWhenTheExplorerNeverReports(t *testing.T) {
	explorer := &scriptedExplorer{replies: []*Msg{finalMsg(explorerProse), finalMsg(explorerProse)}}
	a := exploreAgent(t, explorer)
	out, err := a.explore(context.Background(), "where is expiry checked?", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"answered in prose", "auth/token.go:5", "return exp > now"} {
		if !strings.Contains(out, want) {
			t.Fatalf("result lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Refresh") || strings.Contains(out, "## Rapor") {
		t.Fatalf("unverified prose reached the deciding model:\n%s", out)
	}
}
