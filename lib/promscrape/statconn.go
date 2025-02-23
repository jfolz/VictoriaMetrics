package promscrape

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/netutil"
	"github.com/VictoriaMetrics/metrics"
)

func statStdDial(ctx context.Context, _, addr string) (net.Conn, error) {
	d := getStdDialer()
	network := netutil.GetTCPNetwork()
	conn, err := d.DialContext(ctx, network, addr)
	dialsTotal.Inc()
	if err != nil {
		dialErrors.Inc()
		if !netutil.TCP6Enabled() && !isTCPv4Addr(addr) {
			err = fmt.Errorf("%w; try -enableTCP6 command-line flag if you scrape ipv6 addresses", err)
		}
		return nil, err
	}
	conns.Inc()
	sc := &statConn{
		Conn: conn,
	}
	return sc, nil
}

func getStdDialer() *net.Dialer {
	stdDialerOnce.Do(func() {
		stdDialer = &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: netutil.TCP6Enabled(),
		}
	})
	return stdDialer
}

var (
	stdDialer     *net.Dialer
	stdDialerOnce sync.Once
)

var (
	dialsTotal = metrics.NewCounter(`vm_promscrape_dials_total`)
	dialErrors = metrics.NewCounter(`vm_promscrape_dial_errors_total`)
	conns      = metrics.NewCounter(`vm_promscrape_conns`)
)

type statConn struct {
	closed uint64
	net.Conn
}

func (sc *statConn) Read(p []byte) (int, error) {
	n, err := sc.Conn.Read(p)
	connReadsTotal.Inc()
	if err != nil {
		connReadErrors.Inc()
	}
	connBytesRead.Add(n)
	return n, err
}

func (sc *statConn) Write(p []byte) (int, error) {
	n, err := sc.Conn.Write(p)
	connWritesTotal.Inc()
	if err != nil {
		connWriteErrors.Inc()
	}
	connBytesWritten.Add(n)
	return n, err
}

func (sc *statConn) Close() error {
	err := sc.Conn.Close()
	if atomic.AddUint64(&sc.closed, 1) == 1 {
		conns.Dec()
	}
	return err
}

var (
	connReadsTotal   = metrics.NewCounter(`vm_promscrape_conn_reads_total`)
	connWritesTotal  = metrics.NewCounter(`vm_promscrape_conn_writes_total`)
	connReadErrors   = metrics.NewCounter(`vm_promscrape_conn_read_errors_total`)
	connWriteErrors  = metrics.NewCounter(`vm_promscrape_conn_write_errors_total`)
	connBytesRead    = metrics.NewCounter(`vm_promscrape_conn_bytes_read_total`)
	connBytesWritten = metrics.NewCounter(`vm_promscrape_conn_bytes_written_total`)
)

func isTCPv4Addr(addr string) bool {
	s := addr
	for i := 0; i < 3; i++ {
		n := strings.IndexByte(s, '.')
		if n < 0 {
			return false
		}
		if !isUint8NumString(s[:n]) {
			return false
		}
		s = s[n+1:]
	}
	n := strings.IndexByte(s, ':')
	if n < 0 {
		return false
	}
	if !isUint8NumString(s[:n]) {
		return false
	}
	s = s[n+1:]

	// Verify TCP port
	n, err := strconv.Atoi(s)
	if err != nil {
		return false
	}
	return n >= 0 && n < (1<<16)
}

func isUint8NumString(s string) bool {
	n, err := strconv.Atoi(s)
	if err != nil {
		return false
	}
	return n >= 0 && n < (1<<8)
}
