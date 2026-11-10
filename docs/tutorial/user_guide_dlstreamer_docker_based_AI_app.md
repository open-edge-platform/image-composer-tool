# Example Workload: Docker‑Based Chat Q&A, Face Recognition & DL Streamer Face Detection

*An example guide for running Docker-based AI workloads (Ollama chat, face recognition, DL Streamer face detection) on a Linux image created with OS Image Composer.*

In this tutorial, we create an OS image using **OS Image Composer** with the packages required to run **Docker** and **DL Streamer–based** container images, and then validate the image by running a basic containerized AI application. To enable this, add the Docker/DL Streamer packages to your **YAML user template** before building the image—for example, on RPM-based images include `moby-engine` and `docker-compose`, and on Debian-based images include `docker.io` and `docker-compose-plugin`. Once the image is built and booted on the target, follow the steps below to run the basic application. After Docker is working on your composed image, you can run additional Intel AI applications by following the guides in [`edge-ai-libraries`](https://github.com/open-edge-platform/edge-ai-libraries) and [`edge-ai-suites`](https://github.com/open-edge-platform/edge-ai-suites).
---

## 1. Prerequisites
- Add the DL Streamer and Docker-based packages in the OS Image Composer build configuration using its [Multiple Package Repository Support](../architecture/os-image-composer-multi-repo-support.md) feature

  After the image is built and the system is booted, verify the runtime environment.
  First, confirm that Docker Engine is installed and running.
  Next, confirm that Intel DL Streamer is installed by checking that the directory '/opt/intel/dlstreamer' exists.

---

## 2. Proxy Configuration (Generic Templates)

> Replace placeholders with your organization values:
>
> - `<HTTP_PROXY_URL>` — e.g., `http://proxy.example.com:8080`
> - `<HTTPS_PROXY_URL>` — e.g., `http://proxy.example.com:8443`
> - `<NO_PROXY_LIST>` — e.g., `localhost,127.0.0.1,::1,*.example.com,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,/var/run/docker.sock`

### 2.1 Configure Docker Engine (systemd)

```bash
sudo mkdir -p /etc/systemd/system/docker.service.d

sudo tee /etc/systemd/system/docker.service.d/http-proxy.conf <<'EOF'
[Service]
Environment="HTTP_PROXY=<HTTP_PROXY_URL>"
Environment="HTTPS_PROXY=<HTTPS_PROXY_URL>"
Environment="NO_PROXY=<NO_PROXY_LIST>"
EOF

sudo systemctl daemon-reload
sudo systemctl restart docker
```

### 2.2 Configure Docker CLI (`~/.docker/config.json`)

```bash
mkdir -p ~/.docker

tee ~/.docker/config.json <<'EOF'
{
  "proxies": {
    "default": {
      "httpProxy":  "<HTTP_PROXY_URL>",
      "httpsProxy": "<HTTPS_PROXY_URL>",
      "noProxy":    "<NO_PROXY_LIST>"
    }
  }
}
EOF
```

---

## 3. Chat Q&A with Ollama (Docker)

### 3.1 Start the container

```bash
sudo docker run -d --name ollama \
  --mount source=ollama-data,target=/root/.ollama \
  --memory="4g" --cpus="1" \
  -e HTTP_PROXY="<HTTP_PROXY_URL>" \
  -e HTTPS_PROXY="<HTTPS_PROXY_URL>" \
  -e NO_PROXY="<NO_PROXY_LIST>" \
  ollama/ollama
```

### 3.2 Pull a lightweight model

```bash
sudo docker exec -it ollama ollama pull llama3.2:1b
```

### 3.3 Start interactive chat

```bash
sudo docker exec -it ollama ollama run llama3.2:1b
```

> Tip: For one-shot queries, you can pass a prompt: `ollama run llama3.2:1b -p "Hello"`

---

## 4. Basic Face Recognition (Docker)

### 4.1 Run the container and enter shell

```bash
sudo docker run -it aaftio/face_recognition /bin/bash
```

### 4.2 Prepare folders & sample images (inside container)

```bash
mkdir -p /images/known /images/unknown

# Known faces
wget -P /images/known https://raw.githubusercontent.com/ageitgey/face_recognition/master/examples/biden.jpg
wget -P /images/known https://raw.githubusercontent.com/ageitgey/face_recognition/master/examples/obama.jpg

# Unknown images
wget -P /images/unknown https://raw.githubusercontent.com/ageitgey/face_recognition/master/examples/two_people.jpg
wget -P /images/unknown https://raw.githubusercontent.com/ageitgey/face_recognition/master/examples/alex-lacamoire.png

# Note: For production or security-sensitive environments, verify downloaded files.
# Example (replace <EXPECTED_SHA256_* > with known-good hashes):
# echo "<EXPECTED_SHA256_BIDEN>  /images/known/biden.jpg" | sha256sum -c -
# echo "<EXPECTED_SHA256_OBAMA>  /images/known/obama.jpg" | sha256sum -c -
# echo "<EXPECTED_SHA256_TWO_PEOPLE>  /images/unknown/two_people.jpg" | sha256sum -c -
# echo "<EXPECTED_SHA256_ALEX_LACAMOIRE>  /images/unknown/alex-lacamoire.png" | sha256sum -c -
```

### 4.3 Match faces (inside container)

```bash
face_recognition /images/known /images/unknown/alex-lacamoire.png
face_recognition /images/known /images/unknown/two_people.jpg
```

---

## 5. DL Streamer – Face Detection Pipeline 

Run face detection on a video file using **Open Model Zoo**’s *Face Detection ADAS‑0001* model.

### 5.1 Environment (DL Streamer)

```bash
export GST_PLUGIN_PATH=/opt/intel/dlstreamer/lib:/opt/intel/dlstreamer/gstreamer/lib/gstreamer-1.0:/opt/intel/dlstreamer/streamer/lib/

export LD_LIBRARY_PATH=/opt/intel/dlstreamer/gstreamer/lib:/opt/intel/dlstreamer/lib:/opt/intel/dlstreamer/lib/gstreamer-1.0:/usr/lib:/usr/local/lib/gstreamer-1.0:/usr/local/lib

export PATH=/opt/intel/dlstreamer/gstreamer/bin:/opt/intel/dlstreamer/bin:$PATH

export MODELS_PATH=/home/${USER}/intel/models
```

Verify plugins:
```bash
gst-inspect-1.0 | grep -E "gvadetect|gvawatermark|gvatrack|gvaclassify"
```

### 5.2 Download the model (OMZ tools)

> If you don’t have OMZ tools: `python3 -m pip install openvino-dev` (use a venv if your distro enforces PEP 668).

```bash
omz_downloader --name face-detection-adas-0001
```

The IR will be available at (example):
```
~/intel/face-detection-adas-0001/FP32/face-detection-adas-0001.xml
```

### 5.3 Run the pipeline (save to WebM)

```bash
gst-launch-1.0 filesrc location=/path/to/face-demographics-walking.mp4 ! \
  decodebin ! videoconvert ! \
  gvadetect model=/home/<YOUR_USERNAME>/intel/face-detection-adas-0001/FP32/face-detection-adas-0001.xml device=CPU ! \
  gvawatermark ! videoconvert ! \
  vp8enc ! webmmux ! \
  filesink location=face_detected_output.webm
```

### 5.4 Alternative: Display on screen

```bash
gst-launch-1.0 filesrc location=/path/to/face-demographics-walking.mp4 ! \
  decodebin ! videoconvert ! \
  gvadetect model=/path/to/models/intel/face-detection-adas-0001/FP32/face-detection-adas-0001.xml device=CPU ! \
  gvawatermark ! videoconvert ! \
  autovideosink
```

---

## 6. Notes & Troubleshooting

- **Proxy cert errors (Docker pulls)**: import your corporate root CA into the OS trust store (for example, using `sudo apt-get install -y ca-certificates` and `sudo update-ca-certificates`) and into `/etc/docker/certs.d/<registry>/ca.crt`, then run `systemctl restart docker`.
- **No GVA plugins?** Ensure DL Streamer is installed and `GST_PLUGIN_PATH` is exported.
- **Headless systems**: prefer the file-output pipeline (WebM/MP4) instead of `autovideosink`.
- **Model path errors**: ensure `.xml` and `.bin` are co-located in the same `FP32`/`FP16` folder.
## Additional DL Streamer Applications & Examples

For more DL Streamer (DLS) pipelines, advanced video analytics, multi-model graphs, and edge AI applications, refer to these official Open Edge Platform resources:

- **Open Edge Platform Edge AI Libraries (DL Streamer pipelines & assets):**  
  https://github.com/open-edge-platform/edge-ai-libraries

- **Open Edge Platform Edge AI Suites (end-to-end reference suites / sample solutions):**  
  https://github.com/open-edge-platform/edge-ai-suites

### Edge AI Libraries highlights

This repository contains:
- Ready‑to‑run DL Streamer pipelines  
- Comprehensive model‑proc files  
- Multi-stage pipelines (detect → track → classify → action recognition)  
- Optimized GStreamer graphs for edge deployments  
- Reusable components for real‑time video analytics  
- Integrations with OpenVINO, VA-API, and hardware accelerators  

Use these examples to extend your application beyond basic face detection into:
- Person/vehicle tracking  
- Object classification  
- Action recognition  
- Multi-camera pipelines  
- Custom edge AI applications  

### Edge AI Suites highlights

Edge AI Suites provides:
- End-to-end edge AI sample suites and reference implementations
- Deployment-oriented examples (including containerized flows)
- Pre-integrated building blocks to accelerate solution bring-up on edge platforms

---

## 7. License

This guide contains example commands and scripts provided for convenience. Review third‑party container/images licenses before redistribution.









