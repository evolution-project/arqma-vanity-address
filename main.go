package main

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	flag "github.com/ogier/pflag"
	"hash/crc32"
	genutils "github.com/paxos-bankchain/moneroutil"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const electrumSize = 1626

type keyPair struct {
	Priv *genutils.Key
	Pub  *genutils.Key
}

type wallet struct {
	SpendKey *keyPair
	ViewKey  *keyPair
}

func (k *keyPair) Regen() {
	var reduceFrom [genutils.KeyLength * 2]byte
	rand.Read(reduceFrom[:])
	//copy(reduceFrom[:], tmp)
	genutils.ScReduce(k.Priv, &reduceFrom)
	k.Pub = k.Priv.PubKey()
}

func newKeyPair() *keyPair {
	priv, pub := genutils.NewKeyPair()
	return &keyPair{Priv: priv, Pub: pub}
}

func worker(k chan *keyPair, s chan struct{}, numeral int, vanity string) {
	generated := 0
	nc := fmt.Sprintf("%d", numeral)

	for {
		spend := newKeyPair()
		pbuf := spend.Pub.ToBytes()
		scratch := append(genutils.Uint64ToBytes(0x2cca), pbuf[:]...)
		slug := genutils.EncodeMoneroBase58(scratch[:])

		if slug[3:3+len(vanity)] == vanity && (numeral == 0 || slug[3:3+1] == nc) {
			k <- spend
			return
		}
		generated++
		if generated >= 100 {
			s <- struct{}{}
			generated = 0
		}
	}
}

func (w *wallet) Address() string {
	prefix := genutils.Uint64ToBytes(0x2cca)
	csum := genutils.GetChecksum(prefix, w.SpendKey.Pub[:], w.ViewKey.Pub[:])
	return genutils.EncodeMoneroBase58(prefix, w.SpendKey.Pub[:],
		w.ViewKey.Pub[:], csum[:4])
}

func (w *wallet) Print() {
	spbuf := w.SpendKey.Priv.ToBytes()
	vpbuf := w.ViewKey.Priv.ToBytes()
	fmt.Printf("[!] Address: %s\n", w.Address())
	fmt.Printf("[!] SpendKey: %s\n", hex.EncodeToString(spbuf[:]))
	fmt.Printf("[!] ViewKey: %s\n", hex.EncodeToString(vpbuf[:]))
	fmt.Println("Seed: ");
	b := spbuf[:]
        var words []string
        for i := 0; i < 32; i += 4 {
                val := binary.LittleEndian.Uint32(b[i : i+4])
                w1 := val % electrumSize
                w2 := ((val/electrumSize) + w1) % electrumSize
                w3 := (val/electrumSize/electrumSize + w2) % electrumSize

		words = append(words, electrum_words[w1], electrum_words[w2], electrum_words[w3])
        }
	words = append(words, words[create_checksum_index(words)])
	words = append(words, electrum_words[create_checksum_index2(words)])

	fmt.Println(strings.Join(words[:25], " "))
}

func create_checksum_index(words []string) int {
	h := crc32.NewIEEE()
	for _, word := range words {
		// uniq_prefix EN=3
		if len(word) > 3 {
			h.Write([]byte(word[:3]))
		} else {
			h.Write([]byte(word))
		}
	}
	crc := h.Sum(nil)
	crci := int(binary.BigEndian.Uint32(crc))
	return crci % 24
}

func create_checksum_index2(words []string) int {
	h := crc32.NewIEEE()
	for _, word := range words {
		// uniq_prefix EN=3
		if len(word) > 3 {
			h.Write([]byte(word[:3]))
		} else {
			h.Write([]byte(word))
		}
	}
	crc := h.Sum(nil)
	crci := int(binary.BigEndian.Uint32(crc))
	return crci % len(electrum_words)
}

func main() {
	var w wallet
	var threads int
	var cores int
	numeral := 0

	flag.IntVar(&threads, "threads", runtime.GOMAXPROCS(0),
		"set the number of threads to use")
	flag.IntVar(&cores, "cores", 0,
		"set the number of cores the machine has (not usually required)")
	flag.IntVar(&numeral, "numeral", 0, "set the leading numeral in the address")
	flag.Parse()

	if numeral >= 7 || numeral < 0 {
		fmt.Printf("Cannot produce addresses with a leading numeral of %d\n", numeral)
		return
	}

	if cores > 0 {
		runtime.GOMAXPROCS(cores)
	}

	re := regexp.MustCompile(`^[1-9A-HJ-NP-Za-km-z]+$`).MatchString
	if re(flag.Arg(0)) == false {
		fmt.Printf("Slug has illegal characters: %s\n", flag.Arg(0))
		return
	}

	fmt.Printf("[*] Threads: %d Cores: %d\n", threads, runtime.GOMAXPROCS(0))
	if numeral == 0 {
		fmt.Printf("[*] Searching for Arqma address starting with ar#%s\n", flag.Arg(0))
	} else {
		fmt.Printf("[*] Searching for Arqma address starting with ar%d%s\n", numeral, flag.Arg(0))
	}

	s := make(chan struct{})
	k := make(chan *keyPair)
	for i := 0; i < threads; i++ {
		go worker(k, s, numeral, flag.Arg(0))
	}

	t := time.NewTicker(250 * time.Millisecond)
        generated := 0.0
	for {
		select {
		case w.SpendKey = <-k:

			// Generate Determenistic View Key
			w.ViewKey = &keyPair{Priv: &genutils.Key{}, Pub: nil} // newKeyPair()
			sb := w.SpendKey.Priv.ToBytes()
			w.ViewKey.Priv.FromBytes(genutils.Keccak256(sb[:]))
			genutils.ScReduce32(w.ViewKey.Priv)
			w.ViewKey.Pub = w.ViewKey.Priv.PubKey()

			fmt.Printf("\n")
			w.Print()
			return
		case <-s:
			generated += 100
		case <-t.C:
			fmt.Printf("\r[*] Speed: %f keys/s", generated*4)
			generated = 0.0
		}
	}
}
