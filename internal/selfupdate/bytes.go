package selfupdate

import "fmt"

// HumanBytes formats a byte count as a human-readable string.
func HumanBytes(b int64) string {
	const (
		kb = 1024
		mb = kb * 1024
	)
	switch {
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
