# ntgram

Telegram for Windows NT 4.0, 2000, XP, Vista and 7.

I have a Pentium III with XP on my desk and wanted to read my chats on it. The official apps don't run there, and doing MTProto with its crypto inside a 1996 Win32 program is a lot of work for a slow result. So ntgram is split in two:

- `gateway/` is a small Go server. It runs on any modern machine in the same network (a Linux box, a Raspberry Pi, a NAS), logs in to Telegram with [gotd](https://github.com/gotd/td) and keeps the session.
- `client/` is a 27 KB Win32 program in C. It talks to the gateway over a plain text protocol and uses only APIs that exist in NT 4.0: no `getaddrinfo`, Winsock 1.1, no common controls.

```
 old PC                        gateway                     Telegram
 ntgram.exe  -- TCP 7100 -->   ntgram-gateway  -- MTProto -->
```

## What works

- login with code, two-step password, or sign up
- chat list with unread counts
- last 50 messages of a chat, new messages arrive live
- sending text
- reconnects by itself when the gateway restarts

Photos, files, stickers and voice messages show up as `[photo]`, `[file]` and so on. Nothing can be downloaded or sent except text. Emoji need a font that has them, so on old systems they are boxes.

## Status

Tested with a real account: login, chat list, history, sending and live messages, with the client on Windows 11. The client has not run on real NT 4.0 or XP yet, so far only its import table is checked.

ntgram does not show sponsored messages in channels. Telegram's API terms ask third-party clients to show them, so everyone uses it with their own api_id and at their own risk.

## Setup

1. Get `api_id` and `api_hash` at https://my.telegram.org/apps.
2. Start the gateway:

   ```
   export TG_APP_ID=12345 TG_APP_HASH=0123456789abcdef NTGRAM_PASSWORD=pick-something
   ./ntgram-gateway-linux-amd64
   ```

   Flags: `-listen :7100`, `-session ntgram.session`. The session file is your Telegram login, keep it private.

3. Copy `ntgram.exe` to the old machine and run it. It asks for the gateway address (`192.168.1.20:7100`), then for the gateway password, phone number and code. The address is saved in `ntgram.ini` next to the exe.

Binaries for both parts are on the releases page.

## Security

The protocol between the client and the gateway is not encrypted. Anyone who can see that traffic can read your messages. Use it in your home network, or put a VPN or SSH tunnel in between. `NTGRAM_PASSWORD` only stops other people on the network from using your gateway, and without it the gateway will warn you on start.

## Building

Gateway (Go 1.25):

```
cd gateway && go build -trimpath
```

Client, with mingw-w64 (`gcc-mingw-w64-i686` on Debian and Ubuntu):

```
make -C client
```

CI builds both and fails if the exe imports anything outside KERNEL32, USER32, GDI32, WSOCK32 and msvcrt.

## Protocol

One line per message, UTF-8, fields separated by tabs. Inside a field `\` is written as `\\`, tab as `\t` and newline as `\n`. Lines end with `\n`, and `\r\n` is accepted. It is simple on purpose, so a client for DOS, Windows 3.11 or anything else with TCP should not take long.

Client to gateway:

```
PASS <password>
PHONE <number>
CODE <code>
PASSWORD <two-step password>
NAME <first> [last]
CHATS
HISTORY <chat> [limit]
SEND <chat> <text>
PING
```

Gateway to client:

```
AUTH need_gateway_password | need_phone | need_code | need_password | need_name | ok
CHAT <chat> <title> <unread>
END CHATS
MSG <chat> <id> <unix time> <from> <text>
END HISTORY <chat>
ERR <text>
PONG
```

Chat ids look like `u123` (user), `c123` (group) or `ch123` (channel or supergroup). `HISTORY` and `SEND` only work for chats the gateway has already seen, so send `CHATS` first. `MSG` lines come both as the answer to `HISTORY` and on their own when something new arrives. `from` is `me` for your own messages.

## License

MIT
