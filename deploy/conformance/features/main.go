// Command features prints the supported Gateway API features of turna as a
// comma separated list for the conformance suite.
package main

import (
	"fmt"
	"strings"

	"github.com/rakunlabs/turna/pkg/provider/kubernetes"
)

func main() {
	fmt.Println(strings.Join(kubernetes.SupportedFeatures(), ","))
}
