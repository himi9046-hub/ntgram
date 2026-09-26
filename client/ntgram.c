#define UNICODE
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <winsock.h>
#include <stdarg.h>
#include <stdlib.h>
#include <string.h>
#include "resource.h"

#define WM_SOCKET (WM_APP + 1)
#define WM_ASK (WM_APP + 2)
#define TIMER_PING 1
#define TIMER_RETRY 2
#define NAME_STEP 4

static const struct {
	const char *state;
	const char *cmd;
	const WCHAR *prompt;
	BOOL secret;
} steps[] = {
	{"need_gateway_password", "PASS", L"Gateway password:", TRUE},
	{"need_phone", "PHONE", L"Your phone number in international format, e.g. +79991234567:", FALSE},
	{"need_code", "CODE", L"Login code (Telegram sent it to your other devices or by SMS):", FALSE},
	{"need_password", "PASSWORD", L"Two-step verification password:", TRUE},
	{"need_name", "NAME", L"There is no account for this number yet. First and last name:", FALSE},
};

struct chat {
	char key[32];
	WCHAR title[128];
	int unread;
};

struct ask {
	const WCHAR *prompt;
	WCHAR *answer;
	int size;
	BOOL secret;
};

static HINSTANCE inst;
static HWND win, chatbox, logbox, inputbox, sendbtn, statusbar;
static SOCKET sock = INVALID_SOCKET;
static char inbuf[65536];
static int inlen;
static int step = -1;
static BOOL online;
static WCHAR server[256], ini[MAX_PATH], lasterr[512];
static struct chat chats[200];
static int nchats;
static char current[32];
static int lastday;

static int utf8_to_wide(const char *s, WCHAR *out, int n)
{
	const unsigned char *p = (const unsigned char *)s;
	int i = 0;

	while (*p && i < n - 2) {
		unsigned c = *p++;
		int more = 0;

		if (c >= 0xf0) {
			c &= 0x07;
			more = 3;
		} else if (c >= 0xe0) {
			c &= 0x0f;
			more = 2;
		} else if (c >= 0xc0) {
			c &= 0x1f;
			more = 1;
		} else if (c >= 0x80) {
			c = 0xfffd;
		}
		for (; more > 0 && (*p & 0xc0) == 0x80; more--)
			c = c << 6 | (*p++ & 0x3f);
		if (more > 0)
			c = 0xfffd;
		if (c >= 0x10000) {
			c -= 0x10000;
			out[i++] = 0xd800 | c >> 10;
			out[i++] = 0xdc00 | (c & 0x3ff);
		} else {
			out[i++] = c;
		}
	}
	out[i] = 0;
	return i;
}

static int wide_to_utf8(const WCHAR *s, char *out, int n)
{
	int i = 0;

	for (; *s && i < n - 4; s++) {
		unsigned c = *s;

		if (c >= 0xd800 && c < 0xdc00 && s[1] >= 0xdc00 && s[1] < 0xe000) {
			c = 0x10000 + ((c - 0xd800) << 10) + (s[1] - 0xdc00);
			s++;
		}
		if (c < 0x80) {
			out[i++] = c;
		} else if (c < 0x800) {
			out[i++] = 0xc0 | c >> 6;
			out[i++] = 0x80 | (c & 0x3f);
		} else if (c < 0x10000) {
			out[i++] = 0xe0 | c >> 12;
			out[i++] = 0x80 | (c >> 6 & 0x3f);
			out[i++] = 0x80 | (c & 0x3f);
		} else {
			out[i++] = 0xf0 | c >> 18;
			out[i++] = 0x80 | (c >> 12 & 0x3f);
			out[i++] = 0x80 | (c >> 6 & 0x3f);
			out[i++] = 0x80 | (c & 0x3f);
		}
	}
	out[i] = 0;
	return i;
}

static void set_status(const WCHAR *s)
{
	SetWindowTextW(statusbar, s);
}

static void retry_later(const WCHAR *why)
{
	WCHAR s[512];

	if (sock != INVALID_SOCKET) {
		closesocket(sock);
		sock = INVALID_SOCKET;
	}
	online = FALSE;
	wsprintfW(s, L"%s Trying again in 5 seconds.", why);
	set_status(s);
	SetTimer(win, TIMER_RETRY, 5000, NULL);
}

static void put(const char *first, ...)
{
	static char buf[16384];
	const char *f;
	va_list ap;
	int len = 0, n = 0, sent = 0;

	if (sock == INVALID_SOCKET)
		return;
	va_start(ap, first);
	for (f = first; f; f = va_arg(ap, const char *)) {
		if (n++)
			buf[len++] = '\t';
		for (; *f && len < (int)sizeof buf - 3; f++) {
			if (*f == '\\' || *f == '\t' || *f == '\n') {
				buf[len++] = '\\';
				buf[len++] = *f == '\t' ? 't' : *f == '\n' ? 'n' : '\\';
			} else if (*f != '\r') {
				buf[len++] = *f;
			}
		}
	}
	va_end(ap);
	buf[len++] = '\n';

	while (sent < len) {
		int r = send(sock, buf + sent, len - sent, 0);

		if (r == SOCKET_ERROR) {
			if (WSAGetLastError() != WSAEWOULDBLOCK) {
				retry_later(L"Lost the gateway.");
				return;
			}
			Sleep(10);
			continue;
		}
		sent += r;
	}
}

static void append(const WCHAR *s)
{
	static WCHAR buf[16400];
	int i = 0, end;

	for (; *s && i < 16384; s++) {
		if (*s == '\n')
			buf[i++] = '\r';
		buf[i++] = *s;
	}
	buf[i] = 0;
	end = GetWindowTextLengthW(logbox);
	SendMessageW(logbox, EM_SETSEL, end, end);
	SendMessageW(logbox, EM_REPLACESEL, FALSE, (LPARAM)buf);
}

static void show_message(unsigned long date, const char *from, const char *text)
{
	static WCHAR body[8192];
	WCHAR day[80], clock[32], who[128];
	ULONGLONG t = (ULONGLONG)date * 10000000 + 116444736000000000ULL;
	FILETIME ft, local;
	SYSTEMTIME st;
	int today;

	ft.dwLowDateTime = (DWORD)t;
	ft.dwHighDateTime = (DWORD)(t >> 32);
	FileTimeToLocalFileTime(&ft, &local);
	FileTimeToSystemTime(&local, &st);

	today = st.wYear * 10000 + st.wMonth * 100 + st.wDay;
	if (today != lastday) {
		GetDateFormatW(LOCALE_USER_DEFAULT, DATE_LONGDATE, &st, NULL, day, 80);
		append(lastday ? L"\n--- " : L"--- ");
		append(day);
		append(L" ---\n");
		lastday = today;
	}

	GetTimeFormatW(LOCALE_USER_DEFAULT, TIME_NOSECONDS, &st, NULL, clock, 32);
	utf8_to_wide(from, who, 128);
	utf8_to_wide(text, body, 8192);
	append(clock);
	append(L"  ");
	append(who);
	append(L": ");
	append(body);
	append(L"\n");
}

static void show_row(int i)
{
	WCHAR s[160];
	int sel = SendMessageW(chatbox, LB_GETCURSEL, 0, 0);

	if (chats[i].unread)
		wsprintfW(s, L"%s (%d)", chats[i].title, chats[i].unread);
	else
		lstrcpyW(s, chats[i].title);
	if (i < SendMessageW(chatbox, LB_GETCOUNT, 0, 0))
		SendMessageW(chatbox, LB_DELETESTRING, i, 0);
	SendMessageW(chatbox, LB_INSERTSTRING, i, (LPARAM)s);
	if (sel == i)
		SendMessageW(chatbox, LB_SETCURSEL, i, 0);
}

static int find_chat(const char *key)
{
	int i;

	for (i = 0; i < nchats; i++)
		if (!strcmp(chats[i].key, key))
			return i;
	return -1;
}

static void open_chat(int i)
{
	WCHAR title[160];

	lstrcpynA(current, chats[i].key, sizeof current);
	SetWindowTextW(logbox, L"");
	lastday = 0;
	chats[i].unread = 0;
	show_row(i);
	SendMessageW(chatbox, LB_SETCURSEL, i, 0);
	wsprintfW(title, L"%s - ntgram", chats[i].title);
	SetWindowTextW(win, title);
	put("HISTORY", current, "50", NULL);
}

static void reload(void)
{
	nchats = 0;
	SendMessageW(chatbox, LB_RESETCONTENT, 0, 0);
	put("CHATS", NULL);
}

static int split(char *s, char **f, int max)
{
	size_t len = strlen(s);
	char *r, *w;
	int n = 0;

	if (len && s[len - 1] == '\r')
		s[len - 1] = 0;
	if (!*s)
		return 0;
	f[n++] = w = s;
	for (r = s; *r; r++) {
		if (*r == '\t' && n < max) {
			*w++ = 0;
			f[n++] = w;
		} else if (*r == '\\' && r[1]) {
			r++;
			*w++ = *r == 'n' ? '\n' : *r == 't' ? '\t' : *r;
		} else {
			*w++ = *r;
		}
	}
	*w = 0;
	return n;
}

static void handle(char *line)
{
	char *f[8];
	int n = split(line, f, 8), i;

	if (n == 0)
		return;

	if (!strcmp(f[0], "AUTH") && n > 1) {
		if (!strcmp(f[1], "ok")) {
			step = -1;
			online = TRUE;
			set_status(L"Online");
			reload();
			return;
		}
		for (i = 0; i < (int)(sizeof steps / sizeof steps[0]); i++) {
			if (!strcmp(f[1], steps[i].state)) {
				step = i;
				PostMessageW(win, WM_ASK, i, 0);
			}
		}
	} else if (!strcmp(f[0], "CHAT") && n > 3) {
		if (nchats == (int)(sizeof chats / sizeof chats[0]))
			return;
		lstrcpynA(chats[nchats].key, f[1], sizeof chats[0].key);
		utf8_to_wide(f[2], chats[nchats].title, 128);
		chats[nchats].unread = atoi(f[3]);
		show_row(nchats++);
	} else if (!strcmp(f[0], "END") && n > 1 && !strcmp(f[1], "CHATS")) {
		if ((i = find_chat(current)) >= 0)
			open_chat(i);
	} else if (!strcmp(f[0], "MSG") && n > 5) {
		if (!strcmp(f[1], current)) {
			show_message(strtoul(f[3], NULL, 10), f[4], f[5]);
		} else if ((i = find_chat(f[1])) >= 0) {
			chats[i].unread++;
			show_row(i);
		}
		if (GetForegroundWindow() != win)
			FlashWindow(win, TRUE);
	} else if (!strcmp(f[0], "ERR") && n > 1) {
		WCHAR s[560];

		utf8_to_wide(f[1], lasterr, 512);
		if (step >= 0) {
			PostMessageW(win, WM_ASK, step, 0);
			return;
		}
		wsprintfW(s, L"Error: %s", lasterr);
		lasterr[0] = 0;
		set_status(s);
	}
}

static void on_read(void)
{
	char *start = inbuf, *nl;
	int r = recv(sock, inbuf + inlen, sizeof inbuf - inlen, 0);

	if (r <= 0)
		return;
	inlen += r;
	while ((nl = memchr(start, '\n', inbuf + inlen - start))) {
		*nl = 0;
		handle(start);
		start = nl + 1;
	}
	inlen -= start - inbuf;
	memmove(inbuf, start, inlen);
	if (inlen == sizeof inbuf)
		inlen = 0;
}

static void connect_gateway(void)
{
	struct sockaddr_in sa;
	struct hostent *he;
	char host[256], *colon;
	WCHAR s[320];

	KillTimer(win, TIMER_RETRY);
	if (sock != INVALID_SOCKET)
		closesocket(sock);
	sock = INVALID_SOCKET;
	online = FALSE;
	step = -1;
	inlen = 0;

	WideCharToMultiByte(CP_ACP, 0, server, -1, host, sizeof host, NULL, NULL);
	memset(&sa, 0, sizeof sa);
	sa.sin_family = AF_INET;
	sa.sin_port = htons(7100);
	if ((colon = strrchr(host, ':'))) {
		*colon = 0;
		sa.sin_port = htons(atoi(colon + 1));
	}
	sa.sin_addr.s_addr = inet_addr(host);
	if (sa.sin_addr.s_addr == INADDR_NONE) {
		if (!(he = gethostbyname(host))) {
			retry_later(L"Cannot find the gateway host.");
			return;
		}
		memcpy(&sa.sin_addr, he->h_addr, sizeof sa.sin_addr);
	}

	sock = socket(AF_INET, SOCK_STREAM, 0);
	WSAAsyncSelect(sock, win, WM_SOCKET, FD_CONNECT | FD_READ | FD_CLOSE);
	wsprintfW(s, L"Connecting to %s...", server);
	set_status(s);
	if (connect(sock, (struct sockaddr *)&sa, sizeof sa) == SOCKET_ERROR && WSAGetLastError() != WSAEWOULDBLOCK)
		retry_later(L"Cannot connect to the gateway.");
}

static INT_PTR CALLBACK ask_proc(HWND dlg, UINT msg, WPARAM wp, LPARAM lp)
{
	static struct ask *a;

	switch (msg) {
	case WM_INITDIALOG:
		a = (struct ask *)lp;
		SetDlgItemTextW(dlg, IDC_PROMPT, a->prompt);
		SetDlgItemTextW(dlg, IDC_ANSWER, a->answer);
		SendDlgItemMessageW(dlg, IDC_ANSWER, EM_LIMITTEXT, a->size - 1, 0);
		if (a->secret)
			SendDlgItemMessageW(dlg, IDC_ANSWER, EM_SETPASSWORDCHAR, '*', 0);
		return TRUE;
	case WM_COMMAND:
		if (LOWORD(wp) == IDOK)
			GetDlgItemTextW(dlg, IDC_ANSWER, a->answer, a->size);
		if (LOWORD(wp) == IDOK || LOWORD(wp) == IDCANCEL)
			EndDialog(dlg, LOWORD(wp));
		return TRUE;
	}
	return FALSE;
}

static BOOL ask(const WCHAR *prompt, WCHAR *answer, int size, BOOL secret)
{
	struct ask a = {prompt, answer, size, secret};

	return DialogBoxParamW(inst, MAKEINTRESOURCEW(IDD_ASK), win, ask_proc, (LPARAM)&a) == IDOK;
}

static void login_step(int i)
{
	WCHAR prompt[640], answer[256] = L"";
	char a[800], *last;

	prompt[0] = 0;
	if (lasterr[0]) {
		lstrcpyW(prompt, lasterr);
		lstrcatW(prompt, L"\n\n");
		lasterr[0] = 0;
	}
	lstrcatW(prompt, steps[i].prompt);
	if (!ask(prompt, answer, 256, steps[i].secret) || !answer[0]) {
		set_status(L"Login cancelled. File > Gateway starts it again.");
		return;
	}
	wide_to_utf8(answer, a, sizeof a);
	last = NULL;
	if (i == NAME_STEP && (last = strchr(a, ' ')))
		*last++ = 0;
	put(steps[i].cmd, a, last, NULL);
	set_status(L"Logging in...");
}

static void choose_gateway(void)
{
	if (!ask(L"Gateway address, host:port (the machine running ntgram-gateway):", server, 256, FALSE) || !server[0])
		return;
	WritePrivateProfileStringW(L"ntgram", L"gateway", server, ini);
	connect_gateway();
}

static void send_input(void)
{
	static char buf[12300];
	WCHAR text[4097];

	if (!online || !current[0])
		return;
	GetWindowTextW(inputbox, text, 4097);
	if (!text[0])
		return;
	wide_to_utf8(text, buf, sizeof buf);
	put("SEND", current, buf, NULL);
	SetWindowTextW(inputbox, L"");
}

static void layout(int w, int h)
{
	int gap = 4, bar = 20, row = 24, btn = 72;
	int left = w * 3 / 10 < 140 ? 140 : w * 3 / 10;
	int x = left + 2 * gap, right = w - x - gap;

	MoveWindow(chatbox, gap, gap, left, h - bar - 2 * gap, TRUE);
	MoveWindow(logbox, x, gap, right, h - bar - row - 3 * gap, TRUE);
	MoveWindow(inputbox, x, h - bar - row - gap, right - btn - gap, row, TRUE);
	MoveWindow(sendbtn, w - gap - btn, h - bar - row - gap, btn, row, TRUE);
	MoveWindow(statusbar, gap, h - bar + 3, w - 2 * gap, bar - 4, TRUE);
}

static HWND child(DWORD ex, const WCHAR *cls, const WCHAR *text, DWORD style, int id)
{
	HWND h = CreateWindowExW(ex, cls, text, WS_CHILD | WS_VISIBLE | style, 0, 0, 0, 0, win, (HMENU)(INT_PTR)id, inst, NULL);

	SendMessageW(h, WM_SETFONT, (WPARAM)GetStockObject(DEFAULT_GUI_FONT), FALSE);
	return h;
}

static LRESULT CALLBACK wnd_proc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp)
{
	switch (msg) {
	case WM_CREATE:
		win = hwnd;
		chatbox = child(WS_EX_CLIENTEDGE, L"LISTBOX", NULL,
			WS_VSCROLL | WS_TABSTOP | LBS_NOTIFY | LBS_NOINTEGRALHEIGHT, IDC_CHATS);
		logbox = child(WS_EX_CLIENTEDGE, L"EDIT", NULL,
			WS_VSCROLL | WS_TABSTOP | ES_MULTILINE | ES_READONLY | ES_AUTOVSCROLL, IDC_LOG);
		inputbox = child(WS_EX_CLIENTEDGE, L"EDIT", NULL, WS_TABSTOP | ES_AUTOHSCROLL, IDC_INPUT);
		sendbtn = child(0, L"BUTTON", L"Send", WS_TABSTOP | BS_DEFPUSHBUTTON, IDOK);
		statusbar = child(0, L"STATIC", NULL, SS_LEFTNOWORDWRAP, IDC_STATUS);
		SendMessageW(logbox, EM_LIMITTEXT, 0, 0);
		SendMessageW(inputbox, EM_LIMITTEXT, 4096, 0);
		SetTimer(hwnd, TIMER_PING, 60000, NULL);
		if (server[0])
			connect_gateway();
		else
			PostMessageW(hwnd, WM_COMMAND, IDM_SERVER, 0);
		return 0;

	case WM_SIZE:
		layout(LOWORD(lp), HIWORD(lp));
		return 0;

	case WM_GETMINMAXINFO:
		((MINMAXINFO *)lp)->ptMinTrackSize.x = 420;
		((MINMAXINFO *)lp)->ptMinTrackSize.y = 300;
		return 0;

	case WM_CTLCOLORSTATIC:
		if ((HWND)lp == logbox) {
			SetBkColor((HDC)wp, GetSysColor(COLOR_WINDOW));
			return (LRESULT)GetSysColorBrush(COLOR_WINDOW);
		}
		break;

	case WM_SETFOCUS:
		SetFocus(inputbox);
		return 0;

	case WM_COMMAND:
		switch (LOWORD(wp)) {
		case IDOK:
			send_input();
			break;
		case IDC_CHATS:
			if (HIWORD(wp) == LBN_SELCHANGE) {
				int i = SendMessageW(chatbox, LB_GETCURSEL, 0, 0);

				if (i >= 0 && i < nchats)
					open_chat(i);
			}
			break;
		case IDM_SERVER:
			choose_gateway();
			break;
		case IDM_RELOAD:
			if (online)
				reload();
			break;
		case IDM_EXIT:
			DestroyWindow(hwnd);
			break;
		}
		return 0;

	case WM_TIMER:
		if (wp == TIMER_RETRY)
			connect_gateway();
		else if (online)
			put("PING", NULL);
		return 0;

	case WM_ASK:
		login_step(wp);
		return 0;

	case WM_SOCKET:
		if ((SOCKET)wp != sock)
			return 0;
		switch (WSAGETSELECTEVENT(lp)) {
		case FD_CONNECT:
			if (WSAGETSELECTERROR(lp))
				retry_later(L"Cannot connect to the gateway.");
			else
				set_status(L"Connected, logging in...");
			break;
		case FD_READ:
			on_read();
			break;
		case FD_CLOSE:
			while (sock != INVALID_SOCKET && inlen < (int)sizeof inbuf) {
				int before = inlen;

				on_read();
				if (inlen == before)
					break;
			}
			retry_later(L"The gateway closed the connection.");
			break;
		}
		return 0;

	case WM_DESTROY:
		PostQuitMessage(0);
		return 0;
	}
	return DefWindowProcW(hwnd, msg, wp, lp);
}

int WINAPI WinMain(HINSTANCE hi, HINSTANCE prev, LPSTR cmdline, int show)
{
	WNDCLASSW wc;
	WSADATA wsa;
	MSG m;
	WCHAR *dot;

	inst = hi;
	if (WSAStartup(MAKEWORD(1, 1), &wsa)) {
		MessageBoxW(NULL, L"Winsock is not available.", L"ntgram", MB_ICONSTOP);
		return 1;
	}

	GetModuleFileNameW(NULL, ini, MAX_PATH);
	if ((dot = wcsrchr(ini, L'.')))
		lstrcpyW(dot, L".ini");
	GetPrivateProfileStringW(L"ntgram", L"gateway", L"", server, 256, ini);

	memset(&wc, 0, sizeof wc);
	wc.lpfnWndProc = wnd_proc;
	wc.hInstance = hi;
	wc.hIcon = LoadIcon(NULL, IDI_APPLICATION);
	wc.hCursor = LoadCursor(NULL, IDC_ARROW);
	wc.hbrBackground = (HBRUSH)(COLOR_BTNFACE + 1);
	wc.lpszMenuName = MAKEINTRESOURCEW(IDM_MAIN);
	wc.lpszClassName = L"ntgram";
	RegisterClassW(&wc);

	CreateWindowW(L"ntgram", L"ntgram", WS_OVERLAPPEDWINDOW,
		CW_USEDEFAULT, CW_USEDEFAULT, 640, 480, NULL, NULL, hi, NULL);
	ShowWindow(win, show);

	while (GetMessageW(&m, NULL, 0, 0) > 0) {
		if (!IsDialogMessageW(win, &m)) {
			TranslateMessage(&m);
			DispatchMessageW(&m);
		}
	}
	WSACleanup();
	return 0;
}
