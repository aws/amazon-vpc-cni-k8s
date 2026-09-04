// Command egress-snat-raceprobe reproduces the egress-cni POSTROUTING
// chain-creation race (issue #3782) against a REAL nf_tables backend using the
// REAL go-iptables wrapper, and validates that the fix in #3826 (NewChain
// failure -> ChainExists postcondition recovery) resolves it.
//
// It MUST be run inside an isolated `unshare --net` network namespace so the
// pristine nat table is not shared with the host, e.g.:
//
//	CGO_ENABLED=0 go build -o /tmp/raceprobe ./hack/egress-snat-raceprobe
//	# unfixed pre-fix loop, forced-collision branch -> every Add fails
//	unshare --net /tmp/raceprobe -mode=unfixed  -n=40 -barrier -supplymissing
//	# fix logic on the same collision -> every collision recovers, state complete
//	unshare --net /tmp/raceprobe -mode=fixed    -n=40 -barrier -supplymissing
//	# actual snat.Add via real ListChains -> never reaches NewChain(POSTROUTING)
//	unshare --net /tmp/raceprobe -mode=realfixed -n=40 -barrier -supplymissing
//	# natural high-concurrency (real ListChains) -> does not reach the collision
//	unshare --net /tmp/raceprobe -mode=unfixed  -n=1000 -supplymissing=false
//
// -supplymissing models the branch snat.Add takes when ListChains returned a
// snapshot WITHOUT POSTROUTING (the reported production condition); on real
// nf_tables ListChains otherwise reports the built-in POSTROUTING even on a
// pristine table, so the natural path never reaches the collision.
package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/aws/amazon-vpc-cni-k8s/cmd/egress-cni-plugin/snat"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper"
	"github.com/coreos/go-iptables/iptables"
)

// iptRules mirrors the unexported snat.iptRules so the "unfixed" replica builds
// exactly the same rule set the production code builds.
func iptRules(target, src net.IP, multicastRange, chain, comment string, useRandomFully, useHashRandom bool) [][]string {
	var rules [][]string
	rules = append(rules, []string{chain, "-d", multicastRange, "-j", "ACCEPT", "-m", "comment", "--comment", comment})
	args := []string{chain, "-j", "SNAT", "--to-source", target.String(), "-m", "comment", "--comment", comment}
	if useRandomFully {
		args = append(args, "--random-fully")
	} else if useHashRandom {
		args = append(args, "--random")
	}
	rules = append(rules, args)
	rules = append(rules, []string{"POSTROUTING", "-s", src.String(), "-j", chain, "-m", "comment", "--comment", comment})
	return rules
}

type addResult struct {
	idx             int
	newChainErrText string // stderr of a failed NewChain (empty if NewChain succeeded)
	newChainFailed  bool
	recovered       bool // fixed path: NewChain failed but ChainExists==true
	finalErr        error
}

// runAdd performs the same three-phase Add the production egress-cni code does:
// build rules, ensure chains exist, append rules. `fixed` toggles the PR #3782
// postcondition recovery. `supplyMissing` skips ListChains and treats every
// rule-chain as absent, faithfully modelling the branch the production code
// takes when ListChains returned a snapshot without POSTROUTING. `bar`, if set,
// is crossed after the (missing-chain) snapshot is fixed and before NewChain, so
// all workers attempt creation from the same observed state.
func runAdd(ipt iptableswrapper.IPTablesIface, nodeIP, src net.IP, multicastRange, chain, comment string, fixed, supplyMissing bool, bar *barrier) addResult {
	res := addResult{}
	useRandomFully, useHashRandom := true, false
	if !ipt.HasRandomFully() {
		useRandomFully, useHashRandom = false, true
	}
	rules := iptRules(nodeIP, src, multicastRange, chain, comment, useRandomFully, useHashRandom)

	existingChains := make(map[string]bool)
	if !supplyMissing {
		chains, err := ipt.ListChains("nat")
		if err != nil {
			res.finalErr = fmt.Errorf("ListChains: %w", err)
			return res
		}
		for _, ch := range chains {
			existingChains[ch] = true
		}
	}

	if bar != nil {
		bar.wait()
	}

	for _, rule := range rules {
		ch := rule[0]
		if !existingChains[ch] {
			if err := ipt.NewChain("nat", ch); err != nil {
				if ch == "POSTROUTING" {
					res.newChainFailed = true
					res.newChainErrText = err.Error()
				}
				if !fixed {
					res.finalErr = err
					return res
				}
				// PR #3782 fix: a concurrent ADD may have created the chain
				// after our snapshot. Verify the resulting state instead of
				// classifying error text.
				createErr := err
				exists, existsErr := ipt.ChainExists("nat", ch)
				if existsErr != nil {
					res.finalErr = errors.Join(createErr, existsErr)
					return res
				}
				if !exists {
					res.finalErr = createErr
					return res
				}
				if ch == "POSTROUTING" {
					res.recovered = true
				}
			}
			existingChains[ch] = true
		}
	}

	for _, rule := range rules {
		ch := rule[0]
		if err := ipt.AppendUnique("nat", ch, rule[1:]...); err != nil {
			res.finalErr = fmt.Errorf("AppendUnique %s: %w", ch, err)
			return res
		}
	}
	return res
}

// barrier is a single-use N-party barrier.
type barrier struct {
	n     int
	mu    sync.Mutex
	count int
	ch    chan struct{}
}

func newBarrier(n int) *barrier { return &barrier{n: n, ch: make(chan struct{})} }

func (b *barrier) wait() {
	b.mu.Lock()
	b.count++
	if b.count == b.n {
		close(b.ch)
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()
	<-b.ch
}

func main() {
	mode := flag.String("mode", "unfixed", "unfixed | fixed | realfixed (realfixed calls the actual snat.Add)")
	n := flag.Int("n", 40, "number of concurrent pod ADDs")
	useBarrier := flag.Bool("barrier", true, "cross a barrier before NewChain to force the collision window")
	supplyMissing := flag.Bool("supplymissing", true, "skip ListChains and model the branch where POSTROUTING was observed absent")
	nodeIPStr := flag.String("nodeip", "192.0.2.1", "SNAT target node IP (any routable-looking IPv4 is fine; state is isolated in the netns)")
	flag.Parse()

	ipt, err := iptableswrapper.NewIPTables(iptables.ProtocolIPv4)
	if err != nil {
		fmt.Println("HARNESS_ERROR init:", err)
		os.Exit(2)
	}

	// Report the pre-state so we can see whether a pristine netns already
	// exposes POSTROUTING via ListChains.
	preChains, preErr := ipt.ListChains("nat")
	fmt.Printf("PRESTATE_LISTCHAINS_ERR=%v\n", preErr)
	hasPost := false
	for _, c := range preChains {
		if c == "POSTROUTING" {
			hasPost = true
		}
	}
	fmt.Printf("PRESTATE_HAS_POSTROUTING=%v\n", hasPost)
	fmt.Printf("PRESTATE_CHAINS=%s\n", strings.Join(preChains, ","))

	multicast := "224.0.0.0/4"
	nodeIP := net.ParseIP(*nodeIPStr)
	if nodeIP == nil {
		fmt.Println("HARNESS_ERROR: invalid -nodeip:", *nodeIPStr)
		os.Exit(2)
	}

	var bar *barrier
	if *useBarrier {
		bar = newBarrier(*n)
	}

	results := make([]addResult, *n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < *n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			src := net.IPv4(169, 254, 172, byte(idx+2))
			chain := fmt.Sprintf("CNI-E4-TEST%03d", idx)
			comment := fmt.Sprintf("cni3782-add-race-%d", idx)
			<-start
			if *mode == "realfixed" {
				// Cross the same barrier for timing parity, then call the real code.
				if bar != nil {
					bar.wait()
				}
				results[idx] = addResult{idx: idx, finalErr: snat.Add(ipt, nodeIP, src, multicast, chain, comment, "prng")}
				return
			}
			r := runAdd(ipt, nodeIP, src, multicast, chain, comment, *mode == "fixed", *supplyMissing, bar)
			r.idx = idx
			results[idx] = r
		}(i)
	}
	close(start)
	wg.Wait()

	collisions, finalErrors, recovered := 0, 0, 0
	var sampleErr string
	for _, r := range results {
		if r.newChainFailed {
			collisions++
			if sampleErr == "" {
				sampleErr = strings.ReplaceAll(r.newChainErrText, "\n", " ")
			}
		}
		if r.recovered {
			recovered++
		}
		if r.finalErr != nil {
			finalErrors++
			if sampleErr == "" {
				sampleErr = strings.ReplaceAll(r.finalErr.Error(), "\n", " ")
			}
		}
	}

	// Final nat state.
	postRules, _ := ipt.List("nat", "POSTROUTING")
	jumpCount := 0
	for _, r := range postRules {
		if strings.Contains(r, "cni3782-add-race-") {
			jumpCount++
		}
	}
	custom := 0
	finalChains, _ := ipt.ListChains("nat")
	sort.Strings(finalChains)
	for _, c := range finalChains {
		if strings.HasPrefix(c, "CNI-E4-TEST") {
			custom++
		}
	}

	fmt.Printf("MODE=%s\n", *mode)
	fmt.Printf("N=%d\n", *n)
	fmt.Printf("BARRIER=%v\n", *useBarrier)
	fmt.Printf("SUPPLY_MISSING=%v\n", *supplyMissing)
	fmt.Printf("NEWCHAIN_POSTROUTING_COLLISIONS=%d\n", collisions)
	fmt.Printf("RECOVERED_VIA_CHAINEXISTS=%d\n", recovered)
	fmt.Printf("FINAL_ADD_ERRORS=%d\n", finalErrors)
	fmt.Printf("FINAL_POSTROUTING_POD_JUMPS=%d\n", jumpCount)
	fmt.Printf("FINAL_CUSTOM_CHAINS=%d\n", custom)
	fmt.Printf("SAMPLE_ERROR=%s\n", sampleErr)

	switch *mode {
	case "unfixed":
		if collisions >= 1 && finalErrors >= 1 && strings.Contains(sampleErr, "Chain already exists") {
			fmt.Println("VERDICT=UNFIXED_RACE_REPRODUCED_ADD_FAILED")
		} else {
			fmt.Println("VERDICT=UNFIXED_NO_COLLISION_OBSERVED")
		}
	case "fixed", "realfixed":
		if finalErrors == 0 && jumpCount == *n && custom == *n {
			fmt.Println("VERDICT=FIXED_ALL_RECOVERED_STATE_COMPLETE")
		} else {
			fmt.Println("VERDICT=FIXED_INVESTIGATE")
		}
	}
}
