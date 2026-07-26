package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/mdp/qrterminal/v3"
)

// PrintShareLinks prints each enabled link and a terminal QR code. It is kept
// at the UI boundary so protocol/subscription packages remain pure data code.
func PrintShareLinks(output io.Writer, links []string) error {
	for _, link := range links {
		if strings.TrimSpace(link) == "" {
			continue
		}
		if _, err := fmt.Fprintln(output, link); err != nil {
			return err
		}
		qrterminal.GenerateHalfBlock(link, qrterminal.M, output)
	}
	return nil
}
