# VideoEncoder

Interfață grafică pentru **FFmpeg** pe Windows – similară cu FFmpegGUI, dar cu **interfață locală și externă** (web, cu parolă), coadă de encodare și progres live.

Un singur fișier `VideoEncoder.exe`, fără instalare, fără dependențe.

## Funcții

- **Interfață locală** – se deschide automat într-o fereastră de aplicație (Edge/Chrome `--app`), fără parolă pe `127.0.0.1`
- **Interfață externă** – același UI din rețea / internet, cu utilizator + parolă (blocare 5 min după 5 încercări greșite)
- **Coadă de joburi** – mai multe fișiere deodată, joburi simultane configurabile, pauză / continuare / anulare / reluare, reordonare
- **Progres live** – procent, fps, viteză, timp rămas, mărime, bitrate, jurnal FFmpeg complet
- **Encodere CPU**: H.264 (x264), H.265 (x265), AV1 (SVT-AV1), VP9, ProRes 422 HQ, MPEG-2
- **Encodere hardware** (detectate și testate automat): NVIDIA NVENC, Intel QuickSync, AMD AMF – H.264 / HEVC / AV1
- **Containere**: MP4, MKV, MOV, TS, WebM, **HLS (m3u8)**, GIF animat, doar audio (MP3, AAC, M4A, FLAC, WAV, Opus)
- Rezoluție, fps, deîntrețesere (bwdif), format pixel, GOP, tune, decodare HW
- Audio: codec, bitrate, canale, frecvență, volum, **normalizare loudness EBU R128 (−23 LUFS)** / −16 / −14
- Tăiere (început/sfârșit), subtitrări (păstrează / elimină / arde în imagine), toate pistele
- Filtre `-vf` / `-af` proprii, argumente suplimentare de intrare/ieșire, intrări URL (http, rtmp, udp…)
- **Previzualizare comandă** FFmpeg în timp real (copiabilă)
- **19 presetări incluse** (inclusiv Broadcast TS 1080p25 CBR, HLS VOD, Remux, normalizare EBU) + presetări proprii salvate
- Browser de fișiere pe server, **upload** din browser (drag & drop) și **descărcare** rezultat (HLS se descarcă ca .zip)
- Analiză fișier cu ffprobe (codec, rezoluție, fps, întrețesut, piste audio/sub)
- **Descărcare automată FFmpeg** (build BtbN GPL) dacă nu e instalat
- Prioritate scăzută pentru FFmpeg (opțional), pornire automată la logare

## Instalare

1. Descarcă `VideoEncoder.exe` într-un folder, ex. `C:\VideoEncoder\`
2. Pornește-l. Se deschide fereastra aplicației. Fereastra neagră (consola) este serverul – las-o deschisă (minimizată); închiderea ei oprește aplicația.
3. Dacă FFmpeg nu e găsit: **Setări → Descarcă / actualizează FFmpeg** (se pune în `C:\VideoEncoder\ffmpeg\`).
   Alternativ, pune `ffmpeg.exe` + `ffprobe.exe` lângă exe sau setează calea în Setări.
4. **Schimbă parola** din Setări (implicit `admin` / `admin`) înainte să deschizi accesul extern.

## Acces extern

- În rețea: `http://IP-PC:8090/`
- Din internet: redirecționează portul **8090 TCP** din router către PC → `http://IP-public:8090/`
- Rulează `firewall-allow.bat` ca Administrator ca să deschizi portul în Windows Firewall (sau acceptă promptul Windows la prima pornire).
- Recomandat pentru internet: pune-l în spatele unui reverse proxy cu HTTPS (nginx). Cererile venite prin proxy (`X-Forwarded-For`) sunt tratate mereu ca externe și cer parolă.

## Parametri linie de comandă

```
VideoEncoder.exe [-port 8090] [-nogui] [-data D:\VideoEncoderData]
```

| Parametru | Rol |
|---|---|
| `-port` | portul HTTP (suprascrie config.json) |
| `-nogui` | nu deschide fereastra (rulare în fundal / server) |
| `-data` | folderul pentru config, joburi, presetări, upload-uri |

`autostart-install.bat` / `autostart-remove.bat` – pornire automată la logare (cu `-nogui`).

## Fișiere create (lângă exe)

| Fișier | Conținut |
|---|---|
| `config.json` | setări (port, utilizator, hash parolă, căi, joburi simultane) |
| `jobs.json` | istoricul cozii |
| `presets.json` | presetările tale |
| `output\` | folderul implicit de ieșire |
| `uploads\` | fișierele încărcate din browser |
| `ffmpeg\` | FFmpeg descărcat automat |
| `videoencoder.log` | jurnal aplicație |

## Compilare din sursă

Necesită Go 1.22+. Pe Windows: `build.bat`. Pe Linux:

```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o VideoEncoder.exe .
```

Un tag `v*` pe GitHub construiește automat release-ul (`.github/workflows/release.yml`).

## Structură

```
main.go          server HTTP, config, rute
auth.go          sesiuni, login, acces local fără parolă
builder.go       construirea comenzii FFmpeg din opțiuni
jobs.go          coada, rularea FFmpeg, parsare progres
handlers.go      API: sistem, setări, fișiere, upload, probe, presetări, descărcare FFmpeg
proc_windows.go  ascundere consolă, prioritate, pauză (NtSuspendProcess), fereastră Edge --app
web/             interfața (HTML/CSS/JS, inclusă în exe)
```
