# Configure Users

This guide walks you through setting up login users for your target OS image using ICT.

## Prerequisites

- Linux environment
- ICT tool configured
- Basic understanding of YAML configuration

## Step 1: Understanding User Configuration

ICT supports two types of user password configuration:

1. **Plaintext passwords** (for development/testing only)
2. **Hashed passwords** (recommended for production)

## Step 2: Generate Password Hashes

For production environments, generate secure password hashes:

```bash
# Generate SHA-512 hash for a password
python3 -c "import crypt; print(crypt.crypt('your_password', crypt.mksalt(crypt.METHOD_SHA512)))"

# Alternative using openssl
openssl passwd -6 your_password

# Interactive password prompt (recommended)
python3 -c "import crypt, getpass; print(crypt.crypt(getpass.getpass(), crypt.mksalt(crypt.METHOD_SHA512)))"
```

**Security Note:** Never commit plaintext passwords to version control.

## Step 3: Configure Users in Your Template

Edit your ICT template YAML file to include user configurations:

```yaml
# Basic user configuration examples
systemConfig:
  ...
  users:
    # Development user with plaintext password (NOT for production)
    - name: user
      password: "user"  # Do not commit real plaintext passwords
      groups: ["sudo"]

    # Production user with hashed password
    - name: admin
      hash_algo: "sha512"
      password: "$6$qisZydr7DPWjCwDk$uiFDXvewTwAqs4H0gO7lRkmc5j2IUiuxSA8Yi.kjN9aLu4w3vysV80mD6C/0DvaBPLYCWU2fJwatYxVASJVL20"
      groups: ["sudo"]
```

## Step 4: Common User Groups

### Common User Groups

When configuring users, assign only groups that exist in a minimal Linux OS installation. Common groups include:

- **`users`** – Standard user group (default for most user accounts)
- **`sudo`** – Sudo access group (for administrative privileges; may be called `wheel` on some distributions)
- **`adm`** – System monitoring and log access (present on many distributions)
- **`audio`** – Access to audio devices
- **`video`** – Access to video devices
- **`dialout`** – Access to serial ports

> **Note:** The availability and purpose of groups can vary by distribution. Avoid specifying groups like `docker`, `plugdev`, or `systemd-journal` unless you know they exist in your target OS.

## Step 5: Build Your OS Image

Run ICT to build your image with the configured users.

## Step 6: Test User Login

Test logging in with your configured users:

```bash
# Switch to a configured user
su - user

# Test sudo access
whoami

# Check user's groups
id
```

## Security Best Practices

1. **Never use plaintext passwords in production**
2. **Use strong, unique passwords for each user**
3. **Regularly rotate passwords**
4. **Assign minimal required group permissions**
5. **Remove or disable unused accounts**
6. **Consider using SSH keys instead of passwords**

## Troubleshooting

**Common Issues:**

1. **User cannot login:** Check password hash generation and syntax
2. **No sudo access:** Verify user is in `wheel` or `sudo` group
3. **Permission denied:** Check group assignments for required resources

**Debugging:**

```bash
# Check if user exists
id username

# Verify password hash
sudo cat /etc/shadow | grep username

# Check group membership
groups username
```
