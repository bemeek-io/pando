CC ?= cc
CFLAGS ?= -O2 -Wall -Wextra

.PHONY: all run clean

all: server

server: src/server.c
	$(CC) $(CFLAGS) -o $@ $<

run: server
	./server

clean:
	rm -f server
