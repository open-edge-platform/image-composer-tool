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
