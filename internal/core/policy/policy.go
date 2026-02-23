package policy

import "net/http"

type Policy func(http.Handler) http.Handler

func Chain(h http.Handler, policies ...Policy) http.Handler {
	for i := len(policies) - 1; i >= 0; i-- {
		if policies[i] == nil {
			continue
		}
		h = policies[i](h)
	}
	return h
}
