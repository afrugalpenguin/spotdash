//go:build !windows

package autostart

// SystemStore is nil off Windows, where there is no Run key. A caller treats
// that as "no autostart option to offer".
func SystemStore() Store { return nil }
