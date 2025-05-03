import os
import requests
from bs4 import BeautifulSoup
from urllib.parse import urljoin, urlparse, quote

# Base URL for relative links
BASE_URL = "https://ottawa.ca/en/living-ottawa/"

# Output directory
os.makedirs("pages_text", exist_ok=True)

# List of extensions to ignore (icons, images, etc.)
IGNORE_EXTENSIONS = ['.ico', '.jpg', '.jpeg', '.png', '.gif', '.svg', '.bmp', '.webp', '.pdf']

# Function to check if the URL points to a valid text content page
def is_valid_content_url(url):
    return not any(url.endswith(ext) for ext in IGNORE_EXTENSIONS)

# Read links from file
with open("links.txt", "r") as f:
    links = [line.strip() for line in f if line.strip()]

for link in links:
    # Construct full URL
    if link.startswith("/"):
        full_url = urljoin(BASE_URL, link)
    else:
        full_url = link

    # Skip non-content URLs (e.g., images, icons, etc.)
    if not is_valid_content_url(full_url):
        print(f"Skipping non-content link: {full_url}")
        continue

    try:
        print(f"Fetching: {full_url}")
        response = requests.get(full_url, timeout=10)
        response.raise_for_status()

        # Extract text with BeautifulSoup
        soup = BeautifulSoup(response.content, "html.parser")
        text = soup.get_text(separator="\n", strip=True)

        # Generate safe filename
        parsed_url = urlparse(link)
        filename = quote(parsed_url.path.strip("/").replace("/", "_")) or "index"
        file_path = os.path.join("pages_text", f"{filename}.txt")

        # Save to file
        with open(file_path, "w", encoding="utf-8") as f:
            f.write(text)
    except Exception as e:
        print(f"Failed to process {full_url}: {e}")

print("All pages processed.")

