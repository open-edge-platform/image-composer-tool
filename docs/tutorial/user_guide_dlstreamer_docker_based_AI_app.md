# Example Workload: Docker‑Based Chat Q&A, Face Recognition & DL Streamer Face Detection

*An example guide for running Docker-based AI workloads (Ollama chat, face recognition, DL Streamer face detection) on a Linux image created with OS Image Composer.*

This tutorial assumes you have already built a base OS image using **OS Image Composer** and want to validate it or extend it with containerized edge‑AI workloads. For full details of the AI applications themselves (models, pipelines, etc.), refer to the corresponding guides in the `edge-ai-libraries` repository; this document focuses on how to deploy and run them on your composed image, including typical proxy and Docker configuration steps.
---

## 1. Prerequisites
- Add DL Streamer and Docker based packages as part of OS Image Composer build process using its [Multiple Package Repository Support](../architecture/os-image-composer-multi-repo-support.md) feature
- OS with Docker Engine installed via and running
- (Optional) Corporate proxy details if you are behind a proxy
- For DL Streamer section: Check whether Intel® DL Streamer is installed by verifying that `/opt/intel/dlstreamer` exists (for example, run `ls /opt/intel/dlstreamer`). If the directory is present, DL Streamer is installed

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

- **Proxy cert errors (Docker pulls)**: import your corporate root CA into the OS trust store and into ` sudo apt-get install -y ca-certificates ,
sudo update-ca-certificates , /etc/docker/certs.d/<registry>/ca.crt`, then `systemctl restart docker`.
- **No GVA plugins?** Ensure DL Streamer is installed and `GST_PLUGIN_PATH` is exported.
- **Headless systems**: prefer the file-output pipeline (WebM/MP4) instead of `autovideosink`.
- **Model path errors**: ensure `.xml` and `.bin` are co-located in the same `FP32`/`FP16` folder.
##  Additional DL Streamer Applications & Examples

For more DL Streamer (DLS) pipelines, advanced video analytics, multi-model graphs, and edge AI applications, refer to the official Open Edge Platform AI Libraries:

 **https://github.com/open-edge-platform/edge-ai-libraries**

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


---

## 7. License

This guide contains example commands and scripts provided for convenience. Review third‑party container/images licenses before redistribution.




