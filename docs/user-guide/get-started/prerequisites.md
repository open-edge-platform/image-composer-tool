# General Build Prerequisites for Image Creation Tools

This document describes the general dependencies for the image creation tools
and the steps to install them.

---

## ukify

1. Install all the required dependencies for `ukify`:

```bash
sudo apt install git python3 python3-cryptography python3-pefile python3-pillow \
  python3-setuptools libssl-dev libzstd-dev uuid-dev gnu-efi python3-packaging \
  libelf-dev lz4 pkg-config meson ninja-build
```

2. Clone the systemd repository for `ukify`, and then check out the version
   you want:

```bash
git clone https://github.com/systemd/systemd.git
cd systemd
git checkout v255
```

3. Install `ukify` by copying the `ukify.py` script to a directory in your PATH:

```bash
cd src/ukify
sudo cp ukify.py /usr/local/bin/ukify
```

4. For environments that require `ukify` in `/usr/bin` (e.g., ICT
   build systems), copy it to `/usr/bin`


```bash
sudo cp /usr/local/bin/ukify /usr/bin/ukify
```

5. Verify the installation by running the following command:

```bash
ukify --help
```
You should see the usage instructions for `ukify`.

---

## mmdebstrap

1. Download the mmdebstrap package:

```bash
wget http://archive.ubuntu.com/ubuntu/pool/universe/m/mmdebstrap/mmdebstrap_1.4.3-6_all.deb
```

2. Install the package:

```bash
sudo dpkg -i mmdebstrap_1.4.3-6_all.deb
```

3. If dpkg reports missing dependencies, you can try to automatically resolve
   them using this command:

```bash
sudo apt --fix-broken install
```

---

## util-linux (lsblk) and e2fsprogs (resize2fs)

Overlay-mode disk resize (`overlayPolicy.allowDiskResize`) requires `lsblk`
from **util-linux >= 2.38** (it reads partition start sectors via
`lsblk -o PATH,START,TYPE`) and a `resize2fs` build new enough to grow the
baseline's filesystem. Ubuntu 22.04 ships util-linux 2.37 and e2fsprogs
1.46.5, and neither is available as a newer version via `apt`. Build both
from source instead.

### Upgrade util-linux (lsblk)

1. Download and build util-linux v2.38:

```bash
wget https://www.kernel.org/pub/linux/utils/util-linux/v2.38/util-linux-2.38.tar.xz
tar -xf util-linux-2.38.tar.xz
cd util-linux-2.38
./configure --prefix=/usr/local
make -j$(nproc)
sudo make install
```

2. Divert the distro `lsblk` and point `/usr/bin/lsblk` at the new build.
   `dpkg-divert` keeps the diversion registered so a later `apt upgrade` of
   util-linux writes to the diverted path instead of clobbering your symlink:

```bash
sudo dpkg-divert --divert /usr/bin/lsblk.distrib --rename /usr/bin/lsblk
sudo ln -s /usr/local/bin/lsblk /usr/bin/lsblk
```

3. Verify the installation:

```bash
lsblk --version
```

### Upgrade e2fsprogs (resize2fs)

1. Install the build dependencies:

```bash
sudo apt-get install -y build-essential pkg-config git
```

2. Clone and build e2fsprogs v1.47.0:

```bash
git clone --depth 1 --branch v1.47.0 https://github.com/tytso/e2fsprogs.git
cd e2fsprogs && mkdir build && cd build
../configure
make
```

3. Install `resize2fs` over the distro version in `/usr/sbin` (the overlay
   resize path shells out only to `resize2fs`):

```bash
sudo make install-libs
sudo install -m0755 resize/resize2fs /usr/sbin/resize2fs
```

4. Verify the installation:

```bash
/usr/sbin/resize2fs 2>&1 | head -1
# expect: resize2fs 1.47.0 (...)
```
