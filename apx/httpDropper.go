package main

import (
	"io"
	"net"
	"sync"
)

type httpDropper struct {
	net.Listener
}

func (hd *httpDropper) Accept() (net.Conn, error) {
	// Just accept the connection, but we'll wrap it
	conn, err := hd.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &peekedConn{Conn: conn}, nil
}

type peekedConn struct {
	net.Conn
	once   sync.Once
	r      io.Reader
	dropMe bool
}

func (c *peekedConn) Read(b []byte) (int, error) {
	// Only run on first read, get first byte
	c.once.Do(func() {
		buf := make([]byte, 1)
		_, err := io.ReadFull(c.Conn, buf)
		// If not TLS byte, mark to drop
		if err != nil || buf[0] != 0x16 {
			c.dropMe = true
			return
		}
		// Join byte back on
		c.r = io.MultiReader(bytesReader(buf), c.Conn)
	})

	if c.dropMe {
		// Drop non-TLS traffic like archipelago.gg
		if tc, ok := c.Conn.(*net.TCPConn); ok {
			tc.CloseRead() // Must close read before closing write, otherwise RST not FIN resp
			tc.CloseWrite()
		} else {
			c.Conn.Close()
		}
		return 0, io.EOF
	}

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
