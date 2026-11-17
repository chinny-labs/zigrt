// SPDX-License-Identifier: Apache-2.0

package problem

import (
	"fmt"
	"strconv"
)

func itoa(n int) string { return strconv.Itoa(n) }

func fmtSscan(s string, a, b *int) (int, error) { return fmt.Sscanf(s, "%d + %d", a, b) }
