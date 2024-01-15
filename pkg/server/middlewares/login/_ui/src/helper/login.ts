import axios from "axios";
import type { Method } from "axios";

const login = async (path: string, data: object, abortController?: AbortController) => {
  let requestConfig: RequestConfig = {
    method: "POST",
    url: path,
    data: data,
    abortController: abortController,
  };

  return requestSender(requestConfig);
};

type RequestConfig = {
  method: Method;
  url: string;
  params?: Record<string, any>;
  data?: any;
  timeout?: number;
  headers?: Record<string, any>;
  abortController?: AbortController;
};

const requestSender = async (config: RequestConfig) => {
  try {
    const response = await axios({
      method: config.method,
      url: config.url,
      params: config.params,
      data: config.data,
      headers: config.headers,
      timeout: config.timeout ?? 3000,
      signal: config.abortController?.signal,
    });

    return response;
  } catch (reason: unknown) {
    throw reason;
  }
};

export { login };
