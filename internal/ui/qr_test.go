package ui

import (
	"strings"
	"testing"
)

func TestPrintShareLinksIncludesLinkAndQRCode(t *testing.T) {
	var output strings.Builder
	if err := PrintShareLinks(&output, []string{"vless://example"}); err != nil {
		t.Fatal(err)
	}
	if output.Len() < len("vless://example")+10 || !strings.Contains(output.String(), "vless://example") {
		t.Fatalf("unexpected QR output: %q", output.String())
	}
}
