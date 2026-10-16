import json
import urllib.request

URL = "http://localhost:8080/files/raw/flags/features.json"


def load_flags() -> dict:
    with urllib.request.urlopen(URL) as resp:  # noqa: S310
        return json.load(resp)


if __name__ == "__main__":
    print(load_flags().get("new_checkout", False))
