//go:build !darwin

package mygo

// fixPrintedPDF returns pdf: the radicals in the text of printed PDFs are a
// quirk of CoreGraphics (pdf_text.go).
func fixPrintedPDF(pdf []byte) []byte { return pdf }
