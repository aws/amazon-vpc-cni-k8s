package networkutils

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
)

// GeneratePodHostVethName generates the name for Pod's host-side veth device.
// The veth name is generated in a way that aligns with the value expected by Calico for NetworkPolicy enforcement.
func GeneratePodHostVethName(prefix string, podNamespace string, podName string) string {
	suffix := GeneratePodHostVethNameSuffix(podNamespace, podName)
	return fmt.Sprintf("%s%s", prefix, suffix)
}

// GeneratePodHostVethNameSuffix generates the name suffix for Pod's hostVeth.
func GeneratePodHostVethNameSuffix(podNamespace string, podName string) string {
	h := sha1.New()
	h.Write([]byte(fmt.Sprintf("%s.%s", podNamespace, podName)))
	return hex.EncodeToString(h.Sum(nil))[:11]
}
