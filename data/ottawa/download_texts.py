import os
import requests
from bs4 import BeautifulSoup
from urllib.parse import urljoin, urlparse, quote

# Base URL
BASE_URL = "https://ottawa.ca/en/living-ottawa/"

# Output directory
os.makedirs("pages_text", exist_ok=True)

# List of extensions to ignore (icons, images, etc.)
IGNORE_EXTENSIONS = ['.ico', '.jpg', '.jpeg', '.png', '.gif', '.svg', '.bmp', '.webp', '.pdf', '.css', '.js']

# Function to check if URL is valid and within BASE_URL
def is_valid_content_url(url):
    return (
        url.startswith(BASE_URL) and
        not any(url.lower().endswith(ext) for ext in IGNORE_EXTENSIONS)
    )

# Read links from file
with open("links.txt", "r") as f:
    links = [line.strip() for line in f if line.strip()]

for link in links:
    # Construct full URL from relative link
    full_url = urljoin(BASE_URL, link) if link.startswith("/") else link

    # Skip links not under BASE_URL or with bad extensions
    if not is_valid_content_url(full_url):
        print(f"Skipping link: {full_url}")
        continue

    try:
        print(f"Fetching: {full_url}")
        response = requests.get(full_url, timeout=10)
        response.raise_for_status()

        # Parse and extract clean text
        soup = BeautifulSoup(response.content, "html.parser")
        text = soup.get_text(separator="\n", strip=True)

        # Generate a safe filename
        parsed_url = urlparse(full_url)
        filename = quote(parsed_url.path.strip("/").replace("/", "_")) or "index"
        file_path = os.path.join("pages_text", f"{filename}.txt")

        # Save to text file
        with open(file_path, "w", encoding="utf-8") as f:
            f.write(text)

    except Exception as e:
        print(f"Failed to process {full_url}: {e}")

