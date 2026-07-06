#!/bin/bash

check() {
    return 0
}

depends() {
    return 0
}

install() {
    inst_simple "$moddir/wait-root.sh" "/sbin/wait-root.sh"
    inst_hook cmdline 5 "$moddir/wait-root.sh"
    inst_hook initqueue 90 "$moddir/wait-root.sh"
    inst_hook pre-mount 91 "$moddir/hello.sh"
}
