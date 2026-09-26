# Mac-build paikallisesti

Sovellus rakennetaan Terminalissa Go-kääntäjällä. Xcode toimittaa Applen
clang-kääntäjän ja macOS SDK:n, joita Wailsin Cocoa/WebKit-käyttöliittymä
tarvitsee. Projektissa ei ole Xcodessa avattavaa `.xcodeproj`-tiedostoa.
Nodea, npm-asennusta tai Wails-komentorivityökalua ei tarvita.

## 1. Työkalut

Tarvitset Go 1.26:n tai uudemman sekä Xcoden tai Xcode Command Line Toolsin.
Paikallinen build kohdistetaan macOS 13.0:aan, joka kattaa myös Go 1.27:n
minimivaatimuksen. Uudempaan Go-pääversioon siirryttäessä tarkista sen
käyttöjärjestelmävaatimukset uudelleen.

Jos Homebrew on jo asennettu:

```bash
brew install go
go version
command -v go
```

Jos Go on jo Homebrew'n asentama mutta liian vanha, käytä `brew upgrade go`.
Jos Homebrew puuttuu, Go:n virallinen macOS-asennuspaketti löytyy
[Go:n lataussivulta](https://go.dev/dl/). Apple Siliconille valitaan arm64,
Intelille amd64.

Kun täysi Xcode on asennettu, avaa se kerran ja anna sen viimeistellä
asennus. Tarkista Terminalissa:

```bash
xcode-select -p
xcrun --find clang
xcrun --show-sdk-path
```

Jos koneessa ei ole kumpaakaan Applen työkalupakettia:

```bash
xcode-select --install
```

Jos aktiivinen työkaluhakemisto on väärä ja Xcode on asennettu tähän polkuun:

```bash
sudo xcode-select --switch /Applications/Xcode.app/Contents/Developer
```

Lisenssivirhe ratkaistaan hyväksymällä Xcoden lisenssi Xcodessa tai
`sudo xcodebuild -license` -komennolla. Go-buildia ei ajeta sudolla.

## 2. Rakenna ja avaa

Siirry projektin juureen eli hakemistoon, jossa `go.mod` ja `Makefile` ovat.
Sulje aikaisempi S3 Bucket Browser ennen uuden buildin avaamista.

```bash
make mac
open "dist/S3 Bucket Browser.app"
```

Ensimmäinen build lataa `go.mod`/`go.sum`-tiedostojen mukaiset riippuvuudet
ja on myöhempiä hitaampi. `make mac` käyttää tämän koneen arkkitehtuuria.
Saman voi tehdä ilman makea: `bash scripts/build-macos.sh`.

Skripti kääntää GUI:n ja CLI:n samaan binääriin, lisää kuvakkeen ja
Info.plistin, tarkistaa minimiversion sekä allekirjoittaa ja tarkistaa
paketin paikallisella ad-hoc-allekirjoituksella. Se korvaa vain generoimansa
`dist/S3 Bucket Browser.app` -paketin onnistuneen buildin jälkeen.
Allekirjoittamiseen ei tarvita maksullista Apple-kehittäjätiliä.

Valmista sovellusta voi käyttää tästä hakemistosta tai kopioida Finderilla
Applications-kansioon. Komentorivitila toimii saman paketin sisältä:

```bash
"dist/S3 Bucket Browser.app/Contents/MacOS/s3b" version
"dist/S3 Bucket Browser.app/Contents/MacOS/s3b" --help
```

Pelkkä binääri ilman sovelluspakettia:

```bash
make build
./bin/s3b version
./bin/s3b
```

`VERSION=1.2.3 make mac` asettaa raportoitavan version. Oletus saadaan
gitistä; `-dirty` tarkoittaa paikallisia muutoksia. Apple-paketin numeromuotoiset
versiokentät sisältävät version alkuosan (esimerkiksi `1.2.3`), ja CLI
raportoi koko version, mukaan lukien mahdollisen beta- tai git-tunnisteen.

## 3. Molemmat Mac-arkkitehtuurit

```bash
make mac-universal
file "dist/S3 Bucket Browser.app/Contents/MacOS/s3b"
```

Syntyy samaan polkuun universal-paketti, jonka binäärissä ovat arm64 ja
x86_64. Pelkkä Intel-build: `bash scripts/build-macos.sh amd64`.
Intel-version kääntäminen ei tarvitse Rosettaa; sen ajaminen Apple
Siliconilla on eri asia. Intel-käyttö tulee varmistaa erikseen.

## 4. Tarkistukset

```bash
make test
plutil -lint "dist/S3 Bucket Browser.app/Contents/Info.plist"
codesign --verify --strict --verbose=2 "dist/S3 Bucket Browser.app"
xcrun vtool -show-build "dist/S3 Bucket Browser.app/Contents/MacOS/s3b"
```

`make test` ajaa Go-testit race-tarkistuksella sekä samoilla macOS-kääntäjän
asetuksilla kuin build. `vtool`-tulosteessa `minos` on 13.0 kummallekin
arkkitehtuurille. SDK-versio saa olla tätä uudempi.

Paikallinen tarkistus 26.9.2026:

| Kohde | Tulos |
|---|---|
| Kone | macOS 15.2, Apple Silicon / arm64 |
| Go | Homebrew-asennus, go1.27.1 darwin/arm64 |
| Apple-työkalut | Xcode 16.3; käytetty SDK 15.2 |
| `make build` | Onnistui korjatulla Makefilella |
| `make mac` | arm64-paketti ja allekirjoituksen tarkistus onnistuivat |
| `make mac-universal` | arm64 + x86_64, molempien `minos` 13.0 |
| CLI paketin sisältä | `version` toimii |
| `make test` | Kaikki Go-testipaketit läpi, race-tarkistus käytössä |
| Työpöytäkäynnistys | `open` onnistui; macOS:n ikkunalistassa sovelluksen pääikkuna; prosessi pysyi käynnissä yli 60 sekunnin käynnistysvahdin rajan |

Ikkunalista todentaa natiivin ikkunan olemassaolon, ei sen koko sisällön
visuaalista oikeellisuutta. Tämä ei ole kaikkien S3-palveluiden, tiedostonsiirtojen tai Macin
työpöytäintegraatioiden hyväksymistesti. macOS 13/14 ja Intel-laitteisto
eivät olleet tässä ajotestissä mukana. CI:hin on lisätty paikalliset
build-komennot ja Mac-testit; niiden GitHub-ajo varmistuu muutoksen
viemisen jälkeen.

## 5. Mikä korjattiin

- `make build` päätteli tyhjän `GOOS`-ympäristömuuttujan Windowsiksi ja
  lisäsi Mac-linkitykseen `-H windowsgui`. Kohde haetaan nyt `go env GOOS`
  -komennolla. Alkuperäinen build epäonnistui Go-linkkerin virheisiin.
- Mac-paketin kokoaminen automatisoitiin yhdeksi komennoksi.
- Go 1.27:n objektikoodi vaati macOS 13:n, mutta vanha ohje linkitti 12.0:aan.
  Tämä tuotti varoituksen uudemmalle macOS:lle rakennetusta objektista.
  Kääntäjän, linkkerin, Info.plistin ja CI-tarkistuksen minimi on nyt 13.0.
- Paikallisen tiedoston URL-testi oletti Windowsin asemakirjaimella alkavan
  polun. Unix-polulla odotusarvoon tuli ylimääräinen kauttaviiva. Testi
  korjattiin; sovelluksen tuottama URL oli jo oikea.
- README:n todentamaton väite vanhojen DMG-ongelmien yhdestä varmuudella
  tunnetusta syystä poistettiin. Paikallinen build ja latauksen
  Gatekeeper-tarkistus ovat eri tarkistuksia.

## 6. Jos käynnistys epäonnistuu

Käynnistä binääri Terminalista, jotta virhetuloste näkyy:

```bash
"dist/S3 Bucket Browser.app/Contents/MacOS/s3b"
```

- `go: command not found`: varmista asennus ja avaa uusi Terminal.
- `invalid active developer path` tai SDK-/clang-virhe: viimeistele Xcoden
  asennus ja tarkista `xcode-select`/`xcrun` yllä olevilla komennoilla.
- `undefined: macosApp` tai vastaava: käytä `make mac`; GUI tarvitsee cgo:n.
- Allekirjoitus ei kelpaa: rakenna paketti uudelleen. Älä muokkaa
  allekirjoitetun `.app`-paketin sisältöä jälkikäteen.
- Vanha sovellus näkyy edelleen: sulje se ja avaa nimenomaan tämän
  projektin `dist`-hakemistossa oleva paketti.

Ilman Applen GUI-työkaluja voi rakentaa pelkän CLI:n:

```bash
CGO_ENABLED=0 go build -tags s3b_headless -o bin/s3b-cli ./cmd/s3b
./bin/s3b-cli --help
```

## Paikallinen allekirjoitus ja jakelu

Ad-hoc-allekirjoitus riittää tämän paikallisen buildin allekirjoituksen
tarkistukseen. Se ei ole Apple Developer ID -allekirjoitus eikä notarointi.
Muualta ladattu sovellus voi saada Gatekeeper-varoituksen. Noudata
[Applen ohjetta sovelluksen avaamisesta](https://support.apple.com/en-au/102445);
README:n aiempi väite, ettei Sequoiassa olisi Open Anyway -mahdollisuutta,
oli virheellinen.

Mac-julkaisuja ei oteta tällä muutoksella käyttöön. Muille jaettava
Developer ID -allekirjoitus/notarointi on erillinen työnkulku, eikä
`scripts/sign-macos.sh`-julkaisupolkua ole testattu tässä sertifikaatilla.
Build-skripti käyttää aina paikallista ad-hoc-allekirjoitusta.
