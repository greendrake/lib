package transcode

import (
	"net/http"
	"net/netip"
	"slices"
)

// AllowFrom serves next to the requests from addresses within prefixes — the connection's remote address, never a
// forwarded header — and 403 to any other. A program with another way of admitting its masters uses that in place of
// AllowFrom.
func AllowFrom(prefixes []netip.Prefix, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remote, err := netip.ParseAddrPort(r.RemoteAddr)
		addr := remote.Addr().Unmap().WithZone("")
		if err != nil || !slices.ContainsFunc(prefixes, func(p netip.Prefix) bool { return p.Contains(addr) }) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
