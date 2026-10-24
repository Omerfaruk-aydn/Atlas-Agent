package task

import "net/url"

func Query(s string) string { return url.QueryEscape(s) }
