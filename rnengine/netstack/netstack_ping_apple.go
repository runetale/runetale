//go:build darwin || ios

package netstack

import (
	"net/netip"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

func (ns *Netstack) sendOutboundUserPing(dstIP netip.Addr, timeout time.Duration) error {
	p, err := probing.NewPinger(dstIP.String())
	if err != nil {
		ns.logger.Logger.Infof("sendICMPPingToIP failed to create pinger: %v", err)
		return err
	}

	p.Timeout = timeout
	p.Count = 1
	p.SetPrivileged(false)

	p.OnSend = func(pkt *probing.Packet) {
		ns.logger.Logger.Infof("sendICMPPingToIP: forwarding ping to %s:", p.Addr())
	}
	p.OnRecv = func(pkt *probing.Packet) {
		ns.logger.Logger.Infof("sendICMPPingToIP: %d bytes pong from %s: icmp_seq=%d time=%v", pkt.Nbytes, pkt.IPAddr, pkt.Seq, pkt.Rtt)
	}
	p.OnFinish = func(stats *probing.Statistics) {
		ns.logger.Logger.Infof("sendICMPPingToIP: done, %d replies received", stats.PacketsRecv)
	}

	return p.Run()
}
