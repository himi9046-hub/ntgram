CC = i686-w64-mingw32-gcc
WINDRES = i686-w64-mingw32-windres
CFLAGS = -Os -std=c99 -Wall -D_WIN32_WINNT=0x0400 -DWINVER=0x0400
LDFLAGS = -mwindows -s
LDLIBS = -lwsock32

ntgram.exe: ntgram.o ntgram.res
	$(CC) $(LDFLAGS) -o $@ ntgram.o ntgram.res $(LDLIBS)

ntgram.o: ntgram.c resource.h

ntgram.res: ntgram.rc resource.h
	$(WINDRES) -O coff -o $@ ntgram.rc

clean:
	rm -f ntgram.exe ntgram.o ntgram.res

.PHONY: clean
