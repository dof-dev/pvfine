package pvf

import (
	"os"
	"testing"
)

func TestReal115CNContentRules(t *testing.T) {
	filename := os.Getenv("PVF_115CN_TESTFILE")
	if filename == "" {
		t.Skip("PVF_115CN_TESTFILE required")
	}
	a, err := Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	if a.ClientVersion() != "115CN" || a.Format() != FormatPaged115CN {
		t.Fatal("115CN client identity was not retained")
	}
	if len(a.strA) != 0 || len(a.strW) == 0 || !a.ContentRules().UTF16Only {
		t.Fatal("115CN string pool rules do not match the fixture")
	}
}
