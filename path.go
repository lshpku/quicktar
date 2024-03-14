package quicktar

func BaseName(path string) string {
	lastSlash := -1
	for i, c := range path {
		if c == '/' {
			lastSlash = i
		}
	}
	if lastSlash == -1 {
		return path
	}
	return path[lastSlash+1:]
}

func Split(path string) []string {
	list := []string{}
	lastSlash := 0
	for i, c := range path {
		if c == '/' {
			list = append(list, path[lastSlash:i])
			lastSlash = i + 1
		}
	}
	return append(list, path[lastSlash:])
}

// Parents returns all parent levels of path.
// The returned array doesn't contain root ("") or path itself.
func Parents(path string) []string {
	list := []string{}
	for i, c := range path {
		if c == '/' {
			list = append(list, path[:i])
		}
	}
	return list
}
