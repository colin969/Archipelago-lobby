package main

import (
	"io"
	"net"
)

type httpDropper struct {
	net.Listener
}

func (hd *httpDropper) Accept() (net.Conn, error) {
	for {
		conn, err := hd.Listener.Accept()
		if err != nil {
			return nil, err
		}

		// Peek at the first byte only
		buf := make([]byte, 1)
		_, err = io.ReadFull(conn, buf)
		if err != nil {
			conn.Close()
			continue
		}

		// TLS ClientHello starts with 0x16 (22)
		if buf[0] != 0x16 {
			// Kill the conn so client sees socket hangup (like archipelago.gg)
			conn.Close()
			continue
		}

		// Rejoin the byte onto the connection and pass to the normal handler
		return &peekedConn{conn, io.MultiReader(
			bytesReader(buf), conn,
		)}, nil
	}
}

type peekedConn struct {
	net.Conn
	r io.Reader
}

func (c *peekedConn) Read(b []byte) (int, error) {
	return c.r.Read(b)
}

func bytesReader(b []byte) io.Reader {
	return &singleByteReader{b: b}
}

type singleByteReader struct{ b []byte }

func (r *singleByteReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}
