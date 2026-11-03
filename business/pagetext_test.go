package business

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noisyPage(t *testing.T) string {
	t.Helper()
	page, err := os.ReadFile("testdata/noisy-page.md")
	require.NoError(t, err)
	return string(page)
}

func TestCleaningKeepsTheArticleBody(t *testing.T) {
	cleaned := CleanPageText(noisyPage(t))

	for _, kept := range []string{
		"Der Zug fährt künftig mit Wasserstoff",
		"Die Bahn hat angekündigt, auf zwei Nebenstrecken im Norden künftig Züge mit Wasserstoffantrieb einzusetzen.",
		"Reichweite und Kosten",
		"Pro Tankfüllung sollen die Züge rund 800 Kilometer schaffen.",
		"Kritiker bezweifeln, dass sich der Aufwand gegenüber Oberleitungen lohnt.",
	} {
		assert.Contains(t, cleaned, kept)
	}
}

func TestCleaningReducesLinksToTheirText(t *testing.T) {
	cleaned := CleanPageText(noisyPage(t))

	assert.Contains(t, cleaned, "Wie der Betreiber in einer Mitteilung erläutert, sollen die Fahrzeuge aus eigener Produktion stammen.")
	assert.NotContains(t, cleaned, "](", "no link syntax is left")
	assert.NotContains(t, cleaned, "https://", "no address is left")
}

func TestCleaningRemovesImages(t *testing.T) {
	cleaned := CleanPageText(noisyPage(t))

	assert.NotContains(t, cleaned, "![")
	assert.NotContains(t, cleaned, "Base64")
	assert.NotContains(t, cleaned, "data:image")
	assert.NotContains(t, cleaned, "Logo")
}

func TestCleaningDropsMenuLoginAdvertisingAndTrackingLines(t *testing.T) {
	cleaned := CleanPageText(noisyPage(t))

	for _, dropped := range []string{
		"Zur Startseite", "Premium entdecken", "Anmelden", "Registrieren", "Alle Magazine", "Newsletter",
		"Push-Nachrichten", "Dunkles Schema", "${title}", "${content}", "Anzeige", "Weiterlesen nach der Anzeige",
		"kopieren", "×", "Dieses Menü nicht mehr anzeigen", "* * *",
	} {
		assert.NotContains(t, cleaned, dropped)
	}
}

func TestCleaningLeavesOnlyAboutTheBody(t *testing.T) {
	page := noisyPage(t)

	cleaned := CleanPageText(page)

	assert.Less(t, len(cleaned), len(page)/2, "more than half of the page was noise")
	assert.NotContains(t, cleaned, "\n\n\n", "gaps of several blank lines are closed")
	assert.Equal(t, strings.TrimSpace(cleaned), cleaned)
}

func TestCleaningDropsBylinesStrayLabelsAndFoldedMenus(t *testing.T) {
	page := "Der eigentliche Absatz endet hier mit einem Satz.\n\n( mro)\n\nPush\n\nMehr anzeigenWeniger anzeigen\n\nclose notice"

	assert.Equal(t, "Der eigentliche Absatz endet hier mit einem Satz.", CleanPageText(page))
}

// pageIn builds the same kind of noisy page in any language: a menu of links, a login line, an advertisement label,
// the article, a teaser made of links and a footer.
func pageIn(menu, login, advert, article, teaser, footer string) string {
	return "[" + menu + "](https://news.example/)\n\n" +
		"[" + login + "](https://news.example/login) | [" + login + "](https://news.example/register)\n\n" +
		"- [" + menu + "](https://news.example/a)\n- [" + teaser + "](https://news.example/b)\n\n" +
		advert + "\n\n" +
		article + "\n\n" +
		"[![](<Base64-Image-Removed>)](https://news.example/c)\n\n" +
		"[" + teaser + "](https://news.example/d) [" + menu + "](https://news.example/e)\n\n" +
		advert + "\n\n" + footer
}

func TestCleaningDoesNotDependOnTheLanguageOfThePage(t *testing.T) {
	for name, tc := range map[string]struct{ page, article string }{
		"english": {
			pageIn("Home", "Sign in", "Advertisement", "The railway announced that hydrogen trains will run on two regional lines. The trial starts in spring.", "More from the magazine", "Cookie settings"),
			"The railway announced that hydrogen trains will run on two regional lines. The trial starts in spring.",
		},
		"french": {
			pageIn("Accueil", "Se connecter", "Publicité", "La compagnie a annoncé que des trains à hydrogène circuleront sur deux lignes régionales. L’essai débute au printemps.", "Plus du magazine", "Paramètres des cookies"),
			"La compagnie a annoncé que des trains à hydrogène circuleront sur deux lignes régionales. L’essai débute au printemps.",
		},
		"spanish": {
			pageIn("Inicio", "Iniciar sesión", "Publicidad", "La compañía anunció que los trenes de hidrógeno circularán en dos líneas regionales. La prueba comienza en primavera.", "Más de la revista", "Configuración de cookies"),
			"La compañía anunció que los trenes de hidrógeno circularán en dos líneas regionales. La prueba comienza en primavera.",
		},
		"japanese": {
			pageIn("ホーム", "ログイン", "広告", "鉄道会社は、二つの地方路線で水素列車を走らせると発表した。試験運行は春に始まる。", "雑誌の続きを読む", "クッキー設定"),
			"鉄道会社は、二つの地方路線で水素列車を走らせると発表した。試験運行は春に始まる。",
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.article, CleanPageText(tc.page))
		})
	}
}

func TestCleaningAProseParagraphWithLinksKeepsTheParagraph(t *testing.T) {
	cleaned := CleanPageText("Die [Studie](https://example.org/s) und der [Bericht](https://example.org/b) kommen zum gleichen Ergebnis: Es lohnt sich.")

	assert.Equal(t, "Die Studie und der Bericht kommen zum gleichen Ergebnis: Es lohnt sich.", cleaned)
}

func TestCleaningPlainTextIsLeftAlone(t *testing.T) {
	text := "Erster Absatz mit Inhalt.\n\nZweiter Absatz mit mehr Inhalt."

	assert.Equal(t, text, CleanPageText(text))
}

func TestCuttingAnArticleKeepsWholeWordsWithinTheLimit(t *testing.T) {
	text := "Eins zwei drei vier fünf sechs sieben"

	cut := CutText(text, 20)

	assert.Equal(t, "Eins zwei drei vier", cut)
	assert.LessOrEqual(t, len([]rune(cut)), 20)
}

func TestCuttingLeavesAShortArticleAlone(t *testing.T) {
	assert.Equal(t, "kurz", CutText("kurz", 20))
}

func TestCuttingCountsCharactersNotBytes(t *testing.T) {
	cut := CutText("ÄÖÜ ÄÖÜ ÄÖÜ", 5)

	assert.Equal(t, "ÄÖÜ", cut)
}
