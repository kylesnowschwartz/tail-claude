package parser

// Exports for testing
var ParseTimestamp = parseTimestamp
var ScanSessionMetadataExport = scanSessionMetadata

// ScanSessionPreview wraps scanSessionMetadata to match the old (preview, turnCount)
// signature used by external preview tests.
func ScanSessionPreview(path string) (string, int) {
	m := scanSessionMetadata(path)
	return m.firstMsg, m.turnCount
}
