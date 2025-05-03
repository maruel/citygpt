#!/usr/bin/env python3
import sys
import re
from html.parser import HTMLParser
import html

class HTMLTextExtractor(HTMLParser):
    def __init__(self):
        super().__init__()
        self.result = []
        self.skip = False
        self.current_heading = ""

    def handle_starttag(self, tag, attrs):
        if tag in ('style', 'script', 'head', 'meta', 'noscript'):
            self.skip = True
        elif tag in ('h1', 'h2', 'h3', 'h4', 'h5', 'h6'):
            # Mark headings for special formatting
            self.current_heading = tag
        elif tag == 'br':
            self.result.append("")
        elif tag == 'p':
            # Add paragraph break
            if self.result and self.result[-1] != "":
                self.result.append("")

    def handle_endtag(self, tag):
        if tag in ('style', 'script', 'head', 'meta', 'noscript'):
            self.skip = False
        elif tag in ('h1', 'h2', 'h3', 'h4', 'h5', 'h6'):
            self.current_heading = ""
            # Add extra space after headings
            self.result.append("")
        elif tag == 'p':
            # Add paragraph break
            if self.result and self.result[-1] != "":
                self.result.append("")

    def handle_data(self, data):
        if not self.skip:
            # Decode HTML entities and clean up text
            text = html.unescape(data.strip())
            text = re.sub(r'\s+', ' ', text)
            
            if text:
                # Format headings
                if self.current_heading:
                    if self.result and self.result[-1] != "":
                        self.result.append("")
                    text = text.upper() if self.current_heading == 'h1' else text
                
                self.result.append(text)

    def get_text(self):
        # Clean up the result by removing redundant whitespace
        clean_result = []
        prev_empty = False
        
        for line in self.result:
            current_empty = (line == "")
            
            # Don't add consecutive empty lines
            if not (prev_empty and current_empty):
                clean_result.append(line)
                
            prev_empty = current_empty
            
        return "\n".join(clean_result)

def extract_text_from_html(html_content):
    extractor = HTMLTextExtractor()
    try:
        extractor.feed(html_content)
    except Exception as e:
        # Handle parsing errors gracefully
        return f"Error parsing HTML: {str(e)}\n\nRaw content:\n{html_content[:500]}..."
    return extractor.get_text()

if __name__ == "__main__":
    html = sys.stdin.read()
    print(extract_text_from_html(html))
