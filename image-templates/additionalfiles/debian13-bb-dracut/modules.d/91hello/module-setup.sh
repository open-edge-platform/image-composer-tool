#!/bin/bash

check() {
    return 0
}

depends() {
    return 0
}

install() {
    inst_simple "$moddir/initqueue-sample.sh" "/sbin/initqueue-sample.sh"
    inst_hook cmdline 5 "$moddir/initqueue-sample.sh"
    inst_hook initqueue 90 "$moddir/initqueue-sample.sh"
    inst_hook pre-mount 91 "$moddir/hello.sh"
}
