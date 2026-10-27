// sdks/typescript/core/bodySerializer.gen.ts
var jsonBodySerializer = {
  bodySerializer: (body) => JSON.stringify(body, (_key, value) => typeof value === "bigint" ? value.toString() : value)
};

// sdks/typescript/core/serverSentEvents.gen.ts
function createSseClient({
  onRequest,
  onSseError,
  onSseEvent,
  responseTransformer,
  responseValidator,
  sseDefaultRetryDelay,
  sseMaxRetryAttempts,
  sseMaxRetryDelay,
  sseSleepFn,
  url,
  ...options
}) {
  let lastEventId;
  const sleep = sseSleepFn ?? ((ms) => new Promise((resolve) => setTimeout(resolve, ms)));
  const createStream = async function* () {
    let retryDelay = sseDefaultRetryDelay ?? 3e3;
    let attempt = 0;
    const signal = options.signal ?? new AbortController().signal;
    while (true) {
      if (signal.aborted) break;
      attempt++;
      const headers = options.headers instanceof Headers ? options.headers : new Headers(options.headers);
      if (lastEventId !== void 0) {
        headers.set("Last-Event-ID", lastEventId);
      }
      try {
        const requestInit = {
          redirect: "follow",
          ...options,
          body: options.serializedBody,
          headers,
          signal
        };
        let request = new Request(url, requestInit);
        if (onRequest) {
          request = await onRequest(url, requestInit);
        }
        const _fetch = options.fetch ?? globalThis.fetch;
        const response = await _fetch(request);
        if (!response.ok) throw new Error(`SSE failed: ${response.status} ${response.statusText}`);
        if (!response.body) throw new Error("No body in SSE response");
        const reader = response.body.pipeThrough(new TextDecoderStream()).getReader();
        let buffer = "";
        const abortHandler = () => {
          try {
            reader.cancel();
          } catch {
          }
        };
        signal.addEventListener("abort", abortHandler);
        try {
          while (true) {
            const { done, value } = await reader.read();
            if (done) break;
            buffer += value;
            buffer = buffer.replace(/\r\n?/g, "\n");
            const chunks = buffer.split("\n\n");
            buffer = chunks.pop() ?? "";
            for (const chunk of chunks) {
              const lines = chunk.split("\n");
              const dataLines = [];
              let eventName;
              for (const line of lines) {
                if (line.startsWith("data:")) {
                  dataLines.push(line.replace(/^data:\s*/, ""));
                } else if (line.startsWith("event:")) {
                  eventName = line.replace(/^event:\s*/, "");
                } else if (line.startsWith("id:")) {
                  lastEventId = line.replace(/^id:\s*/, "");
                } else if (line.startsWith("retry:")) {
                  const parsed = Number.parseInt(line.replace(/^retry:\s*/, ""), 10);
                  if (!Number.isNaN(parsed)) {
                    retryDelay = parsed;
                  }
                }
              }
              let data;
              let parsedJson = false;
              if (dataLines.length) {
                const rawData = dataLines.join("\n");
                try {
                  data = JSON.parse(rawData);
                  parsedJson = true;
                } catch {
                  data = rawData;
                }
              }
              if (parsedJson) {
                if (responseValidator) {
                  await responseValidator(data);
                }
                if (responseTransformer) {
                  data = await responseTransformer(data);
                }
              }
              onSseEvent?.({
                data,
                event: eventName,
                id: lastEventId,
                retry: retryDelay
              });
              if (dataLines.length) {
                yield data;
              }
            }
          }
        } finally {
          signal.removeEventListener("abort", abortHandler);
          reader.releaseLock();
        }
        break;
      } catch (error) {
        onSseError?.(error);
        if (sseMaxRetryAttempts !== void 0 && attempt >= sseMaxRetryAttempts) {
          break;
        }
        const backoff = Math.min(retryDelay * 2 ** (attempt - 1), sseMaxRetryDelay ?? 3e4);
        await sleep(backoff);
      }
    }
  };
  const stream = createStream();
  return { stream };
}

// sdks/typescript/core/pathSerializer.gen.ts
var separatorArrayExplode = (style) => {
  switch (style) {
    case "label":
      return ".";
    case "matrix":
      return ";";
    case "simple":
      return ",";
    default:
      return "&";
  }
};
var separatorArrayNoExplode = (style) => {
  switch (style) {
    case "form":
      return ",";
    case "pipeDelimited":
      return "|";
    case "spaceDelimited":
      return "%20";
    default:
      return ",";
  }
};
var separatorObjectExplode = (style) => {
  switch (style) {
    case "label":
      return ".";
    case "matrix":
      return ";";
    case "simple":
      return ",";
    default:
      return "&";
  }
};
var serializeArrayParam = ({
  allowReserved,
  explode,
  name,
  style,
  value
}) => {
  if (!explode) {
    const joinedValues2 = (allowReserved ? value : value.map((v) => encodeURIComponent(v))).join(separatorArrayNoExplode(style));
    switch (style) {
      case "label":
        return `.${joinedValues2}`;
      case "matrix":
        return `;${name}=${joinedValues2}`;
      case "simple":
        return joinedValues2;
      default:
        return `${name}=${joinedValues2}`;
    }
  }
  const separator = separatorArrayExplode(style);
  const joinedValues = value.map((v) => {
    if (style === "label" || style === "simple") {
      return allowReserved ? v : encodeURIComponent(v);
    }
    return serializePrimitiveParam({
      allowReserved,
      name,
      value: v
    });
  }).join(separator);
  return style === "label" || style === "matrix" ? separator + joinedValues : joinedValues;
};
var serializePrimitiveParam = ({
  allowReserved,
  name,
  value
}) => {
  if (value === void 0 || value === null) {
    return "";
  }
  if (typeof value === "object") {
    throw new Error(
      "Deeply-nested arrays/objects aren\u2019t supported. Provide your own `querySerializer()` to handle these."
    );
  }
  return `${name}=${allowReserved ? value : encodeURIComponent(value)}`;
};
var serializeObjectParam = ({
  allowReserved,
  explode,
  name,
  style,
  value,
  valueOnly
}) => {
  if (value instanceof Date) {
    return valueOnly ? value.toISOString() : `${name}=${value.toISOString()}`;
  }
  if (style !== "deepObject" && !explode) {
    let values = [];
    Object.entries(value).forEach(([key, v]) => {
      values = [...values, key, allowReserved ? v : encodeURIComponent(v)];
    });
    const joinedValues2 = values.join(",");
    switch (style) {
      case "form":
        return `${name}=${joinedValues2}`;
      case "label":
        return `.${joinedValues2}`;
      case "matrix":
        return `;${name}=${joinedValues2}`;
      default:
        return joinedValues2;
    }
  }
  const separator = separatorObjectExplode(style);
  const joinedValues = Object.entries(value).map(
    ([key, v]) => serializePrimitiveParam({
      allowReserved,
      name: style === "deepObject" ? `${name}[${key}]` : key,
      value: v
    })
  ).join(separator);
  return style === "label" || style === "matrix" ? separator + joinedValues : joinedValues;
};

// sdks/typescript/core/utils.gen.ts
var PATH_PARAM_RE = /\{[^{}]+\}/g;
var defaultPathSerializer = ({ path, url: _url }) => {
  let url = _url;
  const matches = _url.match(PATH_PARAM_RE);
  if (matches) {
    for (const match of matches) {
      let explode = false;
      let name = match.substring(1, match.length - 1);
      let style = "simple";
      if (name.endsWith("*")) {
        explode = true;
        name = name.substring(0, name.length - 1);
      }
      if (name.startsWith(".")) {
        name = name.substring(1);
        style = "label";
      } else if (name.startsWith(";")) {
        name = name.substring(1);
        style = "matrix";
      }
      const value = path[name];
      if (value === void 0 || value === null) {
        continue;
      }
      if (Array.isArray(value)) {
        url = url.replace(match, serializeArrayParam({ explode, name, style, value }));
        continue;
      }
      if (typeof value === "object") {
        url = url.replace(
          match,
          serializeObjectParam({
            explode,
            name,
            style,
            value,
            valueOnly: true
          })
        );
        continue;
      }
      if (style === "matrix") {
        url = url.replace(
          match,
          `;${serializePrimitiveParam({
            name,
            value
          })}`
        );
        continue;
      }
      const replaceValue = encodeURIComponent(
        style === "label" ? `.${value}` : value
      );
      url = url.replace(match, replaceValue);
    }
  }
  return url;
};
var getUrl = ({
  baseUrl,
  path,
  query,
  querySerializer,
  url: _url
}) => {
  const pathUrl = _url.startsWith("/") ? _url : `/${_url}`;
  let url = (baseUrl ?? "") + pathUrl;
  if (path) {
    url = defaultPathSerializer({ path, url });
  }
  let search = query ? querySerializer(query) : "";
  if (search.startsWith("?")) {
    search = search.substring(1);
  }
  if (search) {
    url += `?${search}`;
  }
  return url;
};
function getValidRequestBody(options) {
  const hasBody = options.body !== void 0;
  const isSerializedBody = hasBody && options.bodySerializer;
  if (isSerializedBody) {
    if ("serializedBody" in options) {
      const hasSerializedBody = options.serializedBody !== void 0 && options.serializedBody !== "";
      return hasSerializedBody ? options.serializedBody : null;
    }
    return options.body !== "" ? options.body : null;
  }
  if (hasBody) {
    return options.body;
  }
  return void 0;
}

// sdks/typescript/core/auth.gen.ts
var getAuthToken = async (auth, callback) => {
  const token = typeof callback === "function" ? await callback(auth) : callback;
  if (!token) {
    return;
  }
  if (auth.scheme === "bearer") {
    return `Bearer ${token}`;
  }
  if (auth.scheme === "basic") {
    return `Basic ${btoa(token)}`;
  }
  return token;
};

// sdks/typescript/client/utils.gen.ts
var createQuerySerializer = ({
  parameters = {},
  ...args
} = {}) => {
  const querySerializer = (queryParams) => {
    const search = [];
    if (queryParams && typeof queryParams === "object") {
      for (const name in queryParams) {
        const value = queryParams[name];
        if (value === void 0 || value === null) {
          continue;
        }
        const options = parameters[name] || args;
        if (Array.isArray(value)) {
          const serializedArray = serializeArrayParam({
            allowReserved: options.allowReserved,
            explode: true,
            name,
            style: "form",
            value,
            ...options.array
          });
          if (serializedArray) search.push(serializedArray);
        } else if (typeof value === "object") {
          const serializedObject = serializeObjectParam({
            allowReserved: options.allowReserved,
            explode: true,
            name,
            style: "deepObject",
            value,
            ...options.object
          });
          if (serializedObject) search.push(serializedObject);
        } else {
          const serializedPrimitive = serializePrimitiveParam({
            allowReserved: options.allowReserved,
            name,
            value
          });
          if (serializedPrimitive) search.push(serializedPrimitive);
        }
      }
    }
    return search.join("&");
  };
  return querySerializer;
};
var getParseAs = (contentType) => {
  if (!contentType) {
    return "stream";
  }
  const cleanContent = contentType.split(";")[0]?.trim();
  if (!cleanContent) {
    return;
  }
  if (cleanContent.startsWith("application/json") || cleanContent.endsWith("+json")) {
    return "json";
  }
  if (cleanContent === "multipart/form-data") {
    return "formData";
  }
  if (["application/", "audio/", "image/", "video/"].some((type) => cleanContent.startsWith(type))) {
    return "blob";
  }
  if (cleanContent.startsWith("text/")) {
    return "text";
  }
  return;
};
var checkForExistence = (options, name) => {
  if (!name) {
    return false;
  }
  if (options.headers.has(name) || options.query?.[name] || options.headers.get("Cookie")?.includes(`${name}=`)) {
    return true;
  }
  return false;
};
async function setAuthParams(options) {
  for (const auth of options.security ?? []) {
    if (checkForExistence(options, auth.name)) {
      continue;
    }
    const token = await getAuthToken(auth, options.auth);
    if (!token) {
      continue;
    }
    const name = auth.name ?? "Authorization";
    switch (auth.in) {
      case "query":
        if (!options.query) {
          options.query = {};
        }
        options.query[name] = token;
        break;
      case "cookie":
        options.headers.append("Cookie", `${name}=${token}`);
        break;
      case "header":
      default:
        options.headers.set(name, token);
        break;
    }
  }
}
var buildUrl = (options) => getUrl({
  baseUrl: options.baseUrl,
  path: options.path,
  query: options.query,
  querySerializer: typeof options.querySerializer === "function" ? options.querySerializer : createQuerySerializer(options.querySerializer),
  url: options.url
});
var mergeConfigs = (a, b) => {
  const config = { ...a, ...b };
  if (config.baseUrl?.endsWith("/")) {
    config.baseUrl = config.baseUrl.substring(0, config.baseUrl.length - 1);
  }
  config.headers = mergeHeaders(a.headers, b.headers);
  return config;
};
var headersEntries = (headers) => {
  const entries = [];
  headers.forEach((value, key) => {
    entries.push([key, value]);
  });
  return entries;
};
var mergeHeaders = (...headers) => {
  const mergedHeaders = new Headers();
  for (const header of headers) {
    if (!header) {
      continue;
    }
    const iterator = header instanceof Headers ? headersEntries(header) : Object.entries(header);
    for (const [key, value] of iterator) {
      if (value === null) {
        mergedHeaders.delete(key);
      } else if (Array.isArray(value)) {
        for (const v of value) {
          mergedHeaders.append(key, v);
        }
      } else if (value !== void 0) {
        mergedHeaders.set(
          key,
          typeof value === "object" ? JSON.stringify(value) : value
        );
      }
    }
  }
  return mergedHeaders;
};
var Interceptors = class {
  fns = [];
  clear() {
    this.fns = [];
  }
  eject(id) {
    const index = this.getInterceptorIndex(id);
    if (this.fns[index]) {
      this.fns[index] = null;
    }
  }
  exists(id) {
    const index = this.getInterceptorIndex(id);
    return Boolean(this.fns[index]);
  }
  getInterceptorIndex(id) {
    if (typeof id === "number") {
      return this.fns[id] ? id : -1;
    }
    return this.fns.indexOf(id);
  }
  update(id, fn) {
    const index = this.getInterceptorIndex(id);
    if (this.fns[index]) {
      this.fns[index] = fn;
      return id;
    }
    return false;
  }
  use(fn) {
    this.fns.push(fn);
    return this.fns.length - 1;
  }
};
var createInterceptors = () => ({
  error: new Interceptors(),
  request: new Interceptors(),
  response: new Interceptors()
});
var defaultQuerySerializer = createQuerySerializer({
  allowReserved: false,
  array: {
    explode: true,
    style: "form"
  },
  object: {
    explode: true,
    style: "deepObject"
  }
});
var defaultHeaders = {
  "Content-Type": "application/json"
};
var createConfig = (override = {}) => ({
  ...jsonBodySerializer,
  headers: defaultHeaders,
  parseAs: "auto",
  querySerializer: defaultQuerySerializer,
  ...override
});

// sdks/typescript/client/client.gen.ts
var createClient = (config = {}) => {
  let _config = mergeConfigs(createConfig(), config);
  const getConfig = () => ({ ..._config });
  const setConfig = (config2) => {
    _config = mergeConfigs(_config, config2);
    return getConfig();
  };
  const interceptors = createInterceptors();
  const beforeRequest = async (options) => {
    const opts = {
      ..._config,
      ...options,
      fetch: options.fetch ?? _config.fetch ?? globalThis.fetch,
      headers: mergeHeaders(_config.headers, options.headers),
      serializedBody: void 0
    };
    if (opts.security) {
      await setAuthParams(opts);
    }
    if (opts.requestValidator) {
      await opts.requestValidator(opts);
    }
    if (opts.body !== void 0 && opts.bodySerializer) {
      opts.serializedBody = opts.bodySerializer(opts.body);
    }
    if (opts.body === void 0 || opts.serializedBody === "") {
      opts.headers.delete("Content-Type");
    }
    const resolvedOpts = opts;
    const url = buildUrl(resolvedOpts);
    return { opts: resolvedOpts, url };
  };
  const request = async (options) => {
    const throwOnError = options.throwOnError ?? _config.throwOnError;
    const responseStyle = options.responseStyle ?? _config.responseStyle;
    let request2;
    let response;
    try {
      const { opts, url } = await beforeRequest(options);
      const requestInit = {
        redirect: "follow",
        ...opts,
        body: getValidRequestBody(opts)
      };
      request2 = new Request(url, requestInit);
      for (const fn of interceptors.request.fns) {
        if (fn) {
          request2 = await fn(request2, opts);
        }
      }
      const _fetch = opts.fetch;
      response = await _fetch(request2);
      for (const fn of interceptors.response.fns) {
        if (fn) {
          response = await fn(response, request2, opts);
        }
      }
      const result = {
        request: request2,
        response
      };
      if (response.ok) {
        const parseAs = (opts.parseAs === "auto" ? getParseAs(response.headers.get("Content-Type")) : opts.parseAs) ?? "json";
        if (response.status === 204 || response.headers.get("Content-Length") === "0") {
          let emptyData;
          switch (parseAs) {
            case "arrayBuffer":
            case "blob":
            case "text":
              emptyData = await response[parseAs]();
              break;
            case "formData":
              emptyData = new FormData();
              break;
            case "stream":
              emptyData = response.body;
              break;
            case "json":
            default:
              emptyData = {};
              break;
          }
          return opts.responseStyle === "data" ? emptyData : {
            data: emptyData,
            ...result
          };
        }
        let data;
        switch (parseAs) {
          case "arrayBuffer":
          case "blob":
          case "formData":
          case "text":
            data = await response[parseAs]();
            break;
          case "json": {
            const text = await response.text();
            data = text ? JSON.parse(text) : {};
            break;
          }
          case "stream":
            return opts.responseStyle === "data" ? response.body : {
              data: response.body,
              ...result
            };
        }
        if (parseAs === "json") {
          if (opts.responseValidator) {
            await opts.responseValidator(data);
          }
          if (opts.responseTransformer) {
            data = await opts.responseTransformer(data);
          }
        }
        return opts.responseStyle === "data" ? data : {
          data,
          ...result
        };
      }
      const textError = await response.text();
      let jsonError;
      try {
        jsonError = JSON.parse(textError);
      } catch {
      }
      throw jsonError ?? textError;
    } catch (error) {
      let finalError = error;
      for (const fn of interceptors.error.fns) {
        if (fn) {
          finalError = await fn(finalError, response, request2, options);
        }
      }
      finalError = finalError || {};
      if (throwOnError) {
        throw finalError;
      }
      return responseStyle === "data" ? void 0 : {
        error: finalError,
        request: request2,
        response
      };
    }
  };
  const makeMethodFn = (method) => (options) => request({ ...options, method });
  const makeSseFn = (method) => async (options) => {
    const { opts, url } = await beforeRequest(options);
    return createSseClient({
      ...opts,
      body: opts.body,
      method,
      onRequest: async (url2, init) => {
        let request2 = new Request(url2, init);
        for (const fn of interceptors.request.fns) {
          if (fn) {
            request2 = await fn(request2, opts);
          }
        }
        return request2;
      },
      serializedBody: getValidRequestBody(opts),
      url
    });
  };
  const _buildUrl = (options) => buildUrl({ ..._config, ...options });
  return {
    buildUrl: _buildUrl,
    connect: makeMethodFn("CONNECT"),
    delete: makeMethodFn("DELETE"),
    get: makeMethodFn("GET"),
    getConfig,
    head: makeMethodFn("HEAD"),
    interceptors,
    options: makeMethodFn("OPTIONS"),
    patch: makeMethodFn("PATCH"),
    post: makeMethodFn("POST"),
    put: makeMethodFn("PUT"),
    request,
    setConfig,
    sse: {
      connect: makeSseFn("CONNECT"),
      delete: makeSseFn("DELETE"),
      get: makeSseFn("GET"),
      head: makeSseFn("HEAD"),
      options: makeSseFn("OPTIONS"),
      patch: makeSseFn("PATCH"),
      post: makeSseFn("POST"),
      put: makeSseFn("PUT"),
      trace: makeSseFn("TRACE")
    },
    trace: makeMethodFn("TRACE")
  };
};

// sdks/typescript/client.gen.ts
var client = createClient(createConfig({ baseUrl: "https://veil.nyc" }));

// sdks/typescript/sdk.gen.ts
var getHealth = (options) => (options?.client ?? client).get({ url: "/health", ...options });
var getOpenApi = (options) => (options?.client ?? client).get({ url: "/openapi.json", ...options });
var listItems = (options) => (options?.client ?? client).get({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/items",
  ...options
});
var createItem = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/items",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var importItems = (options) => (options.client ?? client).post({
  bodySerializer: null,
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/import",
  ...options,
  headers: {
    "Content-Type": "application/octet-stream",
    ...options.headers
  }
});
var deleteItem = (options) => (options.client ?? client).delete({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/items/{name}",
  ...options
});
var updateItem = (options) => (options.client ?? client).patch({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/items/{name}",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var archiveItem = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/items/{name}/archive",
  ...options
});
var listGrants = (options) => (options?.client ?? client).get({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/grants",
  ...options
});
var createGrant = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/grants",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var listGroups = (options) => (options?.client ?? client).get({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/groups",
  ...options
});
var createGroup = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/groups",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var listGroupMembers = (options) => (options.client ?? client).get({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/groups/{name}/members",
  ...options
});
var addGroupMember = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/groups/{name}/members",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var removeGroupMember = (options) => (options.client ?? client).delete({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/groups/{name}/members/{kind}/{id}",
  ...options
});
var listAgents = (options) => (options?.client ?? client).get({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/agents",
  ...options
});
var createAgent = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/agents",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var revokeAgent = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/agents/{name}/revoke",
  ...options
});
var listSessions = (options) => (options?.client ?? client).get({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/sessions",
  ...options
});
var createSession = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/sessions",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var useItem = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/use",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var listRequests = (options) => (options?.client ?? client).get({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/requests",
  ...options
});
var streamRequests = (options) => (options?.client ?? client).sse.get({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/requests/stream",
  ...options
});
var approveRequest = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/requests/{id}/approve",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var denyRequest = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/requests/{id}/deny",
  ...options
});
var listEvents = (options) => (options?.client ?? client).get({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/events",
  ...options
});
var vaultReport = (options) => (options?.client ?? client).get({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/report",
  ...options
});
var provision = (options) => (options?.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/provision",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options?.headers
  }
});
var createInvite = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/invites",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var removeMember = (options) => (options.client ?? client).delete({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/members/{id}",
  ...options
});
var demoteOwner = (options) => (options.client ?? client).delete({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/members/{id}/owner",
  ...options
});
var promoteOwner = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/members/{id}/owner",
  ...options
});
var deleteMe = (options) => (options?.client ?? client).delete({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/me",
  ...options
});
var deleteOrg = (options) => (options?.client ?? client).delete({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/org",
  ...options
});
var getBilling = (options) => (options?.client ?? client).get({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/billing",
  ...options
});
var billingWebhook = (options) => (options.client ?? client).post({
  url: "/v1/billing/webhook",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var auditFeed = (options) => (options?.client ?? client).get({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/audit/events",
  ...options
});
var createBillingCheckout = (options) => (options?.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/billing/checkout",
  ...options
});
var unregisterDevice = (options) => (options.client ?? client).delete({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/devices",
  ...options
});
var registerDevice = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/devices",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var rotateItem = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/items/{name}/rotate",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var bindWorkload = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/agents/{name}/bind",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var verifyIssuer = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/workloads/verify-issuer",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var unbindWorkload = (options) => (options.client ?? client).delete({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/agents/{name}/workloads",
  ...options
});
var listAgentWorkloads = (options) => (options.client ?? client).get({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/agents/{name}/workloads",
  ...options
});
var listRotateProviders = (options) => (options?.client ?? client).get({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/rotate/providers",
  ...options
});
var verifyItem = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/items/{name}/verify",
  ...options
});
var listRotateHooks = (options) => (options.client ?? client).get({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/items/{name}/rotate-hooks",
  ...options
});
var setRotateHooks = (options) => (options.client ?? client).put({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/items/{name}/rotate-hooks",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var fireRotateHooks = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/items/{name}/rotate-hooks/fire",
  ...options
});
var getRecoveryShare = (options) => (options?.client ?? client).get({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/recovery/share",
  ...options
});
var enrollRecovery = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/recovery/enroll",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var startRecovery = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/recovery/start",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var listRecoveries = (options) => (options?.client ?? client).get({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/recovery",
  ...options
});
var getRecovery = (options) => (options.client ?? client).get({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/recovery/{id}",
  ...options
});
var completeRecovery = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/recovery/{id}/complete",
  ...options,
  headers: {
    "Content-Type": "application/json",
    ...options.headers
  }
});
var denyRecovery = (options) => (options.client ?? client).post({
  security: [{ scheme: "bearer", type: "http" }],
  url: "/v1/recovery/{id}/deny",
  ...options
});

// sdks/typescript/ambient-oidc.ts
function b64url(bytes) {
  let bin = "";
  for (const b of bytes) {
    bin += String.fromCharCode(b);
  }
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
function b64urlJson(obj) {
  return b64url(new TextEncoder().encode(JSON.stringify(obj)));
}
async function generateDPoPKeyPair() {
  return await crypto.subtle.generateKey({ name: "ECDSA", namedCurve: "P-256" }, false, [
    "sign",
    "verify"
  ]);
}
async function signDPoPProof(opts) {
  const pubJwk = await crypto.subtle.exportKey("jwk", opts.keyPair.publicKey);
  const parsed = new URL(opts.url);
  const htu = `${parsed.protocol}//${parsed.host}${parsed.pathname}`;
  const payload = {
    htm: opts.method.toUpperCase(),
    htu,
    iat: Math.floor(Date.now() / 1e3),
    jti: crypto.randomUUID()
  };
  if (opts.accessToken) {
    const hash = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(opts.accessToken));
    payload.ath = b64url(new Uint8Array(hash));
  }
  const header = {
    typ: "dpop+jwt",
    alg: "ES256",
    jwk: { kty: "EC", crv: "P-256", x: pubJwk.x, y: pubJwk.y }
  };
  const signingInput = `${b64urlJson(header)}.${b64urlJson(payload)}`;
  const sig = await crypto.subtle.sign(
    { name: "ECDSA", hash: "SHA-256" },
    opts.keyPair.privateKey,
    new TextEncoder().encode(signingInput)
  );
  return `${signingInput}.${b64url(new Uint8Array(sig))}`;
}
function getProc() {
  const g = globalThis;
  return g.process;
}
function readTokenFile(path) {
  const fs = getProc()?.getBuiltinModule?.("node:fs");
  if (!fs?.readFileSync) {
    throw new Error("ambientOidcToken: file reading requires a Node.js runtime");
  }
  const value = fs.readFileSync(path, "utf8").trim();
  if (!value) {
    throw new Error(`ambientOidcToken: empty token file ${path}`);
  }
  return value;
}
async function ambientOidcToken(opts) {
  const headerToken = opts?.request?.headers?.get("x-vercel-oidc-token")?.trim();
  if (headerToken) {
    return headerToken;
  }
  const env = getProc()?.env ?? {};
  const vercelFile = env.VERCEL_OIDC_TOKEN_FILE?.trim();
  if (vercelFile) {
    return readTokenFile(vercelFile);
  }
  const vercelToken = env.VERCEL_OIDC_TOKEN?.trim();
  if (vercelToken) {
    return vercelToken;
  }
  const actionsUrl = env.ACTIONS_ID_TOKEN_REQUEST_URL?.trim();
  const actionsBearer = env.ACTIONS_ID_TOKEN_REQUEST_TOKEN?.trim();
  if (actionsUrl && actionsBearer) {
    const aud = encodeURIComponent(opts?.audience ?? "veil");
    const sep = actionsUrl.includes("?") ? "&" : "?";
    const res = await fetch(`${actionsUrl}${sep}audience=${aud}`, {
      headers: { Authorization: `bearer ${actionsBearer}` }
    });
    if (!res.ok) {
      throw new Error(`ambientOidcToken: GitHub Actions OIDC request failed (${res.status})`);
    }
    const data = await res.json();
    const tok = data.value?.trim();
    if (!tok) {
      throw new Error("ambientOidcToken: empty GitHub Actions OIDC token");
    }
    return tok;
  }
  const veilFile = env.VEIL_OIDC_TOKEN_FILE?.trim();
  if (veilFile) {
    return readTokenFile(veilFile);
  }
  const veilToken = env.VEIL_OIDC_TOKEN?.trim();
  if (veilToken) {
    return veilToken;
  }
  return void 0;
}
function createAmbientClient(config) {
  let keyPairPromise;
  const resolveKeyPair = () => {
    if (config?.dpop && typeof config.dpop === "object") {
      return Promise.resolve(config.dpop);
    }
    keyPairPromise ??= generateDPoPKeyPair();
    return keyPairPromise;
  };
  const baseFetch = config?.fetch ?? globalThis.fetch;
  const dpopFetch = config?.dpop ? async (input, init) => {
    const req = new Request(input, init);
    const auth = req.headers.get("Authorization") ?? "";
    const tok = auth.replace(/^(Bearer|DPoP)\s+/i, "").trim();
    const kp = await resolveKeyPair();
    const proof = await signDPoPProof({
      keyPair: kp,
      method: req.method,
      url: req.url,
      accessToken: tok || void 0
    });
    const headers = new Headers(req.headers);
    if (tok) {
      headers.set("Authorization", `DPoP ${tok}`);
    }
    headers.set("DPoP", proof);
    return baseFetch(new Request(req, { headers }));
  } : config?.fetch;
  return createClient({
    baseUrl: "https://veil.nyc",
    ...config,
    ...dpopFetch ? { fetch: dpopFetch } : {},
    auth: async () => await ambientOidcToken(config) ?? ""
  });
}
export {
  addGroupMember,
  ambientOidcToken,
  approveRequest,
  archiveItem,
  auditFeed,
  billingWebhook,
  bindWorkload,
  completeRecovery,
  createAgent,
  createAmbientClient,
  createBillingCheckout,
  createClient,
  createGrant,
  createGroup,
  createInvite,
  createItem,
  createSession,
  deleteItem,
  deleteMe,
  deleteOrg,
  demoteOwner,
  denyRecovery,
  denyRequest,
  enrollRecovery,
  fireRotateHooks,
  generateDPoPKeyPair,
  getBilling,
  getHealth,
  getOpenApi,
  getRecovery,
  getRecoveryShare,
  importItems,
  listAgentWorkloads,
  listAgents,
  listEvents,
  listGrants,
  listGroupMembers,
  listGroups,
  listItems,
  listRecoveries,
  listRequests,
  listRotateHooks,
  listRotateProviders,
  listSessions,
  promoteOwner,
  provision,
  registerDevice,
  removeGroupMember,
  removeMember,
  revokeAgent,
  rotateItem,
  setRotateHooks,
  signDPoPProof,
  startRecovery,
  streamRequests,
  unbindWorkload,
  unregisterDevice,
  updateItem,
  useItem,
  vaultReport,
  verifyIssuer,
  verifyItem
};
