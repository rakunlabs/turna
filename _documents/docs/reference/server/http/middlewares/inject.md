# inject

Inject middleware help to change content of the anything. Give a content-type you want to change it.

```yaml
middlewares:
  test:
    inject:
      path_map:
        "/test": # checking with doublestar.Match
          - regex: "" # old is ignored if regex is set
            old: ""
            new: ""
      content_map: # map of content-type
        "text/html":
          - regex: "" # old is ignored if regex is set
            old: "my text"
            new: "my mext"
```

Example:

```yaml
inject:
  content_map:
    "text/html":
      - old: "</head>"
        new: |
          <script defer>
          // Override the fetch function
          window.fetch = async function(...args) {
            try {
            // Use the original fetch function and get the response
            const response = await originalFetch(...args);

            // Check for the 407 status code
            if (response.status === 407) {
              location.reload();  // Refresh the page
              return;  // Optionally, you can throw an error or return a custom response here
            }

            // Return the original response for other cases
            return response;
            } catch (error) {
            throw error;  // Rethrow any errors that occurred during the fetch
            }
          }
          </script>
          </head>
```
