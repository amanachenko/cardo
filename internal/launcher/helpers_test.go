package launcher

import "net"

// listenLocal opens a listener on an ephemeral loopback port, so a test can make a port genuinely
// busy without relying on a service being installed.
func listenLocal() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}

// freeLocalPort finds a port that was just free. It is used to prove the negative case: BusyPorts
// must report nothing for a port nobody holds.
func freeLocalPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		return 0, err
	}
	return port, nil
}
