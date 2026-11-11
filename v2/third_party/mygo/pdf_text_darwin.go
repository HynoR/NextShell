//go:build darwin

package mygo

import "github.com/egoist/mygo/internal/darwin"

// fixPrintedPDF gives the text of a PDF that WebKit printed the characters
// it names radicals for (pdf_text.go).
func fixPrintedPDF(pdf []byte) []byte { return fixPDFText(pdf, darwin.RadicalIdeographs) }
