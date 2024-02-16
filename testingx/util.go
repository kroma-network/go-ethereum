package testingx

import (
	"strings"
	"testing"
)

func IsZk(t *testing.T) bool { return strings.HasSuffix(t.Name(), "Zk") }
