package main

func stripUnattendedFlag(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if isUnattendedFlag(arg) {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func isUnattendedFlag(arg string) bool {
	switch arg {
	case "-u", "--u", "-u=true", "-u=false", "--u=true", "--u=false":
		return true
	default:
		return false
	}
}
