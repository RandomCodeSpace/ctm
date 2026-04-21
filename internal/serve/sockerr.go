package serve

import (
	"errors"
	"io"
	"net/http"
	"syscall"
)

// isAddrInUse reports whether err is the kernel's "address already in
// use" condition. Works on Linux + macOS via errno unwrapping; nothing
// platform-specific.
func isAddrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}

// probeIsCtmServe issues a short-timeout GET /healthz against addr and
// returns true when the response carries the X-Ctm-Serve header. Used
// by the single-instance guard after a failed bind so we can
// distinguish "another ctm serve is up" (silent success) from "some
// other process owns the port" (refuse).
func probeIsCtmServe(addr string) bool {
	client := &http.Client{Timeout: probeTimeout}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		return false
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	return resp.Header.Get(ServeVersionHeader) != ""
}
