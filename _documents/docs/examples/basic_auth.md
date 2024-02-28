# basic_auth

```yaml
server:
  entrypoints:
    web:
      address: ":8000"
  http:
    middlewares:
      basic_auth:
        basic_auth:
          users:
            - "test:$apr1$JMWtQHoL$g/5ey5x7psJM7htuB6OEy0" # pass
            - "test2:$apr1$u4NQ6Doq$KdCzBPfjarcQ0mk4Fd/3v1" # pass
      myfolder:
        folder:
          # path: ./testdata/html
          # index: true
          # spa: true
          browse: true
          utc: true
    routers:
      private:
        path: /pkg/server/*
        middlewares:
          - basic_auth
          - myfolder
      test:
        path: /*
        middlewares:
          - myfolder
```
