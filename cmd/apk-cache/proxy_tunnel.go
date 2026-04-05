package main

import (
	"io"
	"net"
)

func tunnelCopy(dst net.Conn, src net.Conn) {
	defer dst.Close()
	defer src.Close()
	_, _ = io.Copy(dst, src)
}
