package browser

// FindChrome returns the path of the first Chrome or Chromium install it
// can see, or the empty string.
func FindChrome() string { return findChrome() }
