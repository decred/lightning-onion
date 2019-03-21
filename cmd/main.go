package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrec/secp256k1"
	sphinx "github.com/decred/lightning-onion"
)

// main implements a simple command line utility that can be used in order to
// either generate a fresh mix-header or decode and fully process an existing
// one given a private key.
func main() {
	args := os.Args

	assocData := bytes.Repeat([]byte{'B'}, 32)

	if len(args) < 2 {
		fmt.Printf("Usage: %s (generate|decode) <private-keys>\n", args[0])
		os.Exit(1)
	}

	switch args[1] {
	case "generate":
		var route []*secp256k1.PublicKey
		for i, hexKey := range args[2:] {
			binKey, err := hex.DecodeString(hexKey)
			if err != nil || len(binKey) != 33 {
				log.Fatalf("%s is not a valid hex pubkey %s", hexKey, err)
			}

			pubkey, err := secp256k1.ParsePubKey(binKey)
			if err != nil {
				panic(err)
			}

			route = append(route, pubkey)
			fmt.Fprintf(os.Stderr, "Node %d pubkey %x\n", i, pubkey.SerializeCompressed())
		}

		sessionKey, _ := secp256k1.PrivKeyFromBytes(bytes.Repeat([]byte{'A'}, 32))

		var hopsData []sphinx.HopData
		for i := 0; i < len(route); i++ {
			hopsData = append(hopsData, sphinx.HopData{
				Realm:         0x00,
				ForwardAmount: uint64(i),
				OutgoingCltv:  uint32(i),
			})
			copy(hopsData[i].NextAddress[:], bytes.Repeat([]byte{byte(i)}, 8))
		}

		msg, err := sphinx.NewOnionPacket(route, sessionKey, hopsData, assocData)
		if err != nil {
			log.Fatalf("Error creating message: %v", err)
		}

		w := bytes.NewBuffer([]byte{})
		err = msg.Encode(w)

		if err != nil {
			log.Fatalf("Error serializing message: %v", err)
		}

		fmt.Printf("%x\n", w.Bytes())
	case "decode":
		binKey, err := hex.DecodeString(args[2])
		if len(binKey) != 32 || err != nil {
			log.Fatalf("Argument not a valid hex private key")
		}

		hexBytes, _ := ioutil.ReadAll(os.Stdin)
		binMsg, err := hex.DecodeString(strings.TrimSpace(string(hexBytes)))
		if err != nil {
			log.Fatalf("Error decoding message: %s", err)
		}

		privkey, _ := secp256k1.PrivKeyFromBytes(binKey)
		s := sphinx.NewRouter(privkey, &chaincfg.TestNet3Params,
			sphinx.NewMemoryReplayLog())

		var packet sphinx.OnionPacket
		err = packet.Decode(bytes.NewBuffer(binMsg))

		if err != nil {
			log.Fatalf("Error parsing message: %v", err)
		}
		p, err := s.ProcessOnionPacket(&packet, assocData, 10)
		if err != nil {
			log.Fatalf("Failed to decode message: %s", err)
		}

		w := bytes.NewBuffer([]byte{})
		err = p.NextPacket.Encode(w)

		if err != nil {
			log.Fatalf("Error serializing message: %v", err)
		}
		fmt.Printf("%x\n", w.Bytes())
	default:
		fmt.Printf("Usage: %s (generate|decode) <private-keys>\n", args[0])
		os.Exit(1)
	}
}
