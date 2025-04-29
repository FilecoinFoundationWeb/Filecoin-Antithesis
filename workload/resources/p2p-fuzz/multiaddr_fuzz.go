package p2pfuzz

import (
	"fmt"
	"log"
	"math/rand"
	"strings"
)

// Base components for constructing multiaddrs
var baseComponents = []string{
	"/ip4/127.0.0.1",
	"/ip6/::1",
	"/ip6zone/eth0",
	"/tcp/1234",
	"/udp/1234",
	"/dns4/localhost",
	"/dns6/localhost",
	"/dnsaddr/localhost",
	"/p2p/QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N",
}

// A small set of known valid multiaddrs for occasional inclusion
var validMultiaddrs = []string{
	"/ip4/127.0.0.1/tcp/1234",
}

// Special characters to inject into multiaddr components
var specialChars = []string{
	"/../", "//", "\\", "\x00", "\n", "\t", "\r",
	"!", "@", "#", "$", "%", "^", "&", "*", "?",
}

// generateValidMultiaddr creates a structurally valid random multiaddr.
func generateValidMultiaddr() string {
	if rand.Float32() < 0.2 { // 20% chance to return a known good one
		return validMultiaddrs[rand.Intn(len(validMultiaddrs))]
	}

	var parts []string

	if rand.Float32() < 0.5 { // 50% chance for IPv4
		ip := fmt.Sprintf("/ip4/%d.%d.%d.%d",
			rand.Intn(256), rand.Intn(256),
			rand.Intn(256), rand.Intn(256))
		parts = append(parts, ip)
	} else { // 50% chance for IPv6
		ip := fmt.Sprintf("/ip6/%x:%x:%x:%x:%x:%x:%x:%x",
			rand.Intn(65536), rand.Intn(65536), rand.Intn(65536), rand.Intn(65536),
			rand.Intn(65536), rand.Intn(65536), rand.Intn(65536), rand.Intn(65536))
		parts = append(parts, ip)
	}

	port := fmt.Sprintf("/tcp/%d", rand.Intn(65535)+1)
	parts = append(parts, port)

	return strings.Join(parts, "")
}

// generateFuzzedMultiaddr creates potentially invalid or malformed multiaddrs.
func generateFuzzedMultiaddr() string {
	if rand.Float32() < 0.3 { // 30% chance to send a valid one
		addr := generateValidMultiaddr()
		log.Printf("Generated valid multiaddr: %s", addr)
		return addr
	}

	mutationType := rand.Intn(5)

	switch mutationType {
	case 0: // Combine random base components
		numComponents := rand.Intn(4) + 1
		components := make([]string, numComponents)
		for i := 0; i < numComponents; i++ {
			components[i] = baseComponents[rand.Intn(len(baseComponents))]
		}
		return strings.Join(components, "")

	case 1: // Inject special characters
		component := baseComponents[rand.Intn(len(baseComponents))]
		specialChar := specialChars[rand.Intn(len(specialChars))]
		if len(component) == 0 {
			return specialChar
		} // Handle empty component case
		pos := rand.Intn(len(component))
		return component[:pos] + specialChar + component[pos:]

	case 2: // Extremely long value for a protocol
		protocols := []string{"ip4", "ip6", "tcp", "udp", "dns4", "dns6"}
		proto := protocols[rand.Intn(len(protocols))]
		// Limit length to avoid excessive memory use, e.g., max 2048
		value := strings.Repeat(string(rune(rand.Intn(26)+'a')), rand.Intn(2000)+100)
		return fmt.Sprintf("/%s/%s", proto, value)

	case 3: // Invalid IP address format (e.g., octets > 255)
		octet := func() string { return fmt.Sprintf("%d", rand.Intn(512)) }
		return fmt.Sprintf("/ip4/%s.%s.%s.%s", octet(), octet(), octet(), octet())

	default: // Invalid port number (e.g., > 65535)
		port := rand.Intn(70000) + 65535
		return fmt.Sprintf("/tcp/%d", port)
	}
}
