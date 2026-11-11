// Command say says messages of the golden catalog, which TestGoldenBuilds
// runs.
package main

import (
	"fmt"
	"testing/fstest"

	"github.com/egoist/mygo/i18n"
	locales "github.com/egoist/mygo/internal/i18ngen/testdata/app"
)

func main() {
	lib, err := i18n.NewLibraryCatalog(fstest.MapFS{
		"en.json": {Data: []byte(`{"cancel": "Cancel"}`)},
	}, "en", "mygo")
	if err != nil {
		panic(err)
	}
	fmt.Println(locales.Title())
	fmt.Println(locales.In("de").Title())
	fmt.Println(locales.FilesSelected(3))
	fmt.Println(locales.In("de-AT").FilesSelected(0))
	fmt.Println(locales.In("zh-TW").FilesSelected(1))
	fmt.Println(locales.Guests("Ada", 3))
	fmt.Println(lib.Format(i18n.Locale(), "cancel", nil))
	i18n.SetLocaleFunc(func() string { return "de" })
	fmt.Println(lib.Format(i18n.Locale(), "cancel", nil))
	fmt.Println(i18n.Languages())
}
