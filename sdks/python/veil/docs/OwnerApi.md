# veil.OwnerApi

All URIs are relative to *https://veil.nyc*

Method | HTTP request | Description
------------- | ------------- | -------------
[**approve_request**](OwnerApi.md#approve_request) | **POST** /v1/requests/{id}/approve | Approve an open request — unlocks its level-1 grant for ttl. First write wins.
[**archive_item**](OwnerApi.md#archive_item) | **POST** /v1/items/{name}/archive | Hide from Use and list. History stays.
[**create_agent**](OwnerApi.md#create_agent) | **POST** /v1/agents | Register an agent principal. Not a Hydra secret. Not MCP.
[**create_grant**](OwnerApi.md#create_grant) | **POST** /v1/grants | Grant an agent or a Kratos human Use on an item. Same grant object. Not MCP. Not a family vault.
[**create_item**](OwnerApi.md#create_item) | **POST** /v1/items | Create an item. Secret is in the request over TLS. Never in the response. Not MCP.
[**create_session**](OwnerApi.md#create_session) | **POST** /v1/sessions | Mint a short-lived Use lease onto an existing agent. Token is in this response once. Sandbox gets the session file, not the agent JWT. Default 15m, max 1h. Not MCP.
[**delete_item**](OwnerApi.md#delete_item) | **DELETE** /v1/items/{name} | Remove the item and its grants. Not MCP.
[**deny_request**](OwnerApi.md#deny_request) | **POST** /v1/requests/{id}/deny | Deny an open request. First write wins; the agent&#39;s next use files a fresh ask.
[**import_items**](OwnerApi.md#import_items) | **POST** /v1/import | One-shot 1Password .1pux or CSV onto origin. Secret in the file, never in the response. Not MCP.
[**list_agents**](OwnerApi.md#list_agents) | **GET** /v1/agents | Agents in this org. Ids only. Never secrets. Not MCP.
[**list_grants**](OwnerApi.md#list_grants) | **GET** /v1/grants | Grants in this org. No secrets. Not MCP.
[**list_requests**](OwnerApi.md#list_requests) | **GET** /v1/requests | Approval requests filed by level-1 agents. status&#x3D;open lists only unexpired asks. Not MCP.
[**list_sessions**](OwnerApi.md#list_sessions) | **GET** /v1/sessions | Active sandbox sessions. Metadata only. Never the token. Not MCP.
[**revoke_agent**](OwnerApi.md#revoke_agent) | **POST** /v1/agents/{name}/revoke | Revoke an agent. Idempotent. Kills grants, sessions, and in-flight Use. Record stays for audit.
[**stream_requests**](OwnerApi.md#stream_requests) | **GET** /v1/requests/stream | Server-sent events feed of approval-request changes for the owner org. Each event is an empty tick — refetch GET /v1/requests for the authoritative rows. Not MCP.
[**update_item**](OwnerApi.md#update_item) | **PATCH** /v1/items/{name} | Replace URIs, tags, and fill username. No secret.


# **approve_request**
> ApprovalRequest approve_request(id, approve_request_body=approve_request_body)

Approve an open request — unlocks its level-1 grant for ttl. First write wins.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.approval_request import ApprovalRequest
from veil.models.approve_request_body import ApproveRequestBody
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)

# The client must configure the authentication and authorization parameters
# in accordance with the API server security policy.
# Examples for each auth method are provided below, use the example that
# satisfies your auth use case.

# Configure Bearer authorization (JWT): bearerAuth
configuration = veil.Configuration(
    access_token = os.environ["BEARER_TOKEN"]
)

# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.OwnerApi(api_client)
    id = 'id_example' # str | 
    approve_request_body = veil.ApproveRequestBody() # ApproveRequestBody |  (optional)

    try:
        # Approve an open request — unlocks its level-1 grant for ttl. First write wins.
        api_response = api_instance.approve_request(id, approve_request_body=approve_request_body)
        print("The response of OwnerApi->approve_request:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->approve_request: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**|  | 
 **approve_request_body** | [**ApproveRequestBody**](ApproveRequestBody.md)|  | [optional] 

### Return type

[**ApprovalRequest**](ApprovalRequest.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | The approved request |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | not owner |  -  |
**404** | no such request in this org |  -  |
**409** | already resolved or expired |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **archive_item**
> archive_item(name)

Hide from Use and list. History stays.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)

# The client must configure the authentication and authorization parameters
# in accordance with the API server security policy.
# Examples for each auth method are provided below, use the example that
# satisfies your auth use case.

# Configure Bearer authorization (JWT): bearerAuth
configuration = veil.Configuration(
    access_token = os.environ["BEARER_TOKEN"]
)

# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.OwnerApi(api_client)
    name = 'name_example' # str | 

    try:
        # Hide from Use and list. History stays.
        api_instance.archive_item(name)
    except Exception as e:
        print("Exception when calling OwnerApi->archive_item: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **name** | **str**|  | 

### Return type

void (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | archived |  -  |
**400** | bad request |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | agent cannot archive items |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_agent**
> Agent create_agent(create_agent_request)

Register an agent principal. Not a Hydra secret. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.agent import Agent
from veil.models.create_agent_request import CreateAgentRequest
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)

# The client must configure the authentication and authorization parameters
# in accordance with the API server security policy.
# Examples for each auth method are provided below, use the example that
# satisfies your auth use case.

# Configure Bearer authorization (JWT): bearerAuth
configuration = veil.Configuration(
    access_token = os.environ["BEARER_TOKEN"]
)

# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.OwnerApi(api_client)
    create_agent_request = veil.CreateAgentRequest() # CreateAgentRequest | 

    try:
        # Register an agent principal. Not a Hydra secret. Not MCP.
        api_response = api_instance.create_agent(create_agent_request)
        print("The response of OwnerApi->create_agent:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->create_agent: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **create_agent_request** | [**CreateAgentRequest**](CreateAgentRequest.md)|  | 

### Return type

[**Agent**](Agent.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Agent principal. No secret. |  -  |
**400** | bad request |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | not owner |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_grant**
> Grant create_grant(create_grant_request)

Grant an agent or a Kratos human Use on an item. Same grant object. Not MCP. Not a family vault.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.create_grant_request import CreateGrantRequest
from veil.models.grant import Grant
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)

# The client must configure the authentication and authorization parameters
# in accordance with the API server security policy.
# Examples for each auth method are provided below, use the example that
# satisfies your auth use case.

# Configure Bearer authorization (JWT): bearerAuth
configuration = veil.Configuration(
    access_token = os.environ["BEARER_TOKEN"]
)

# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.OwnerApi(api_client)
    create_grant_request = veil.CreateGrantRequest() # CreateGrantRequest | 

    try:
        # Grant an agent or a Kratos human Use on an item. Same grant object. Not MCP. Not a family vault.
        api_response = api_instance.create_grant(create_grant_request)
        print("The response of OwnerApi->create_grant:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->create_grant: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **create_grant_request** | [**CreateGrantRequest**](CreateGrantRequest.md)|  | 

### Return type

[**Grant**](Grant.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Grant |  -  |
**400** | bad request |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | not owner |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_item**
> Item create_item(create_item_request)

Create an item. Secret is in the request over TLS. Never in the response. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.create_item_request import CreateItemRequest
from veil.models.item import Item
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)

# The client must configure the authentication and authorization parameters
# in accordance with the API server security policy.
# Examples for each auth method are provided below, use the example that
# satisfies your auth use case.

# Configure Bearer authorization (JWT): bearerAuth
configuration = veil.Configuration(
    access_token = os.environ["BEARER_TOKEN"]
)

# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.OwnerApi(api_client)
    create_item_request = veil.CreateItemRequest() # CreateItemRequest | 

    try:
        # Create an item. Secret is in the request over TLS. Never in the response. Not MCP.
        api_response = api_instance.create_item(create_item_request)
        print("The response of OwnerApi->create_item:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->create_item: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **create_item_request** | [**CreateItemRequest**](CreateItemRequest.md)|  | 

### Return type

[**Item**](Item.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Item metadata. No secret. |  -  |
**400** | bad request |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | agent cannot create items |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_session**
> CreateSessionResponse create_session(create_session_request)

Mint a short-lived Use lease onto an existing agent. Token is in this response once. Sandbox gets the session file, not the agent JWT. Default 15m, max 1h. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.create_session_request import CreateSessionRequest
from veil.models.create_session_response import CreateSessionResponse
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)

# The client must configure the authentication and authorization parameters
# in accordance with the API server security policy.
# Examples for each auth method are provided below, use the example that
# satisfies your auth use case.

# Configure Bearer authorization (JWT): bearerAuth
configuration = veil.Configuration(
    access_token = os.environ["BEARER_TOKEN"]
)

# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.OwnerApi(api_client)
    create_session_request = veil.CreateSessionRequest() # CreateSessionRequest | 

    try:
        # Mint a short-lived Use lease onto an existing agent. Token is in this response once. Sandbox gets the session file, not the agent JWT. Default 15m, max 1h. Not MCP.
        api_response = api_instance.create_session(create_session_request)
        print("The response of OwnerApi->create_session:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->create_session: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **create_session_request** | [**CreateSessionRequest**](CreateSessionRequest.md)|  | 

### Return type

[**CreateSessionResponse**](CreateSessionResponse.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Session plus token once. |  -  |
**400** | bad request |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | not owner |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_item**
> delete_item(name)

Remove the item and its grants. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)

# The client must configure the authentication and authorization parameters
# in accordance with the API server security policy.
# Examples for each auth method are provided below, use the example that
# satisfies your auth use case.

# Configure Bearer authorization (JWT): bearerAuth
configuration = veil.Configuration(
    access_token = os.environ["BEARER_TOKEN"]
)

# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.OwnerApi(api_client)
    name = 'name_example' # str | 

    try:
        # Remove the item and its grants. Not MCP.
        api_instance.delete_item(name)
    except Exception as e:
        print("Exception when calling OwnerApi->delete_item: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **name** | **str**|  | 

### Return type

void (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | deleted |  -  |
**400** | bad request |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | agent cannot delete items |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **deny_request**
> ApprovalRequest deny_request(id)

Deny an open request. First write wins; the agent's next use files a fresh ask.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.approval_request import ApprovalRequest
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)

# The client must configure the authentication and authorization parameters
# in accordance with the API server security policy.
# Examples for each auth method are provided below, use the example that
# satisfies your auth use case.

# Configure Bearer authorization (JWT): bearerAuth
configuration = veil.Configuration(
    access_token = os.environ["BEARER_TOKEN"]
)

# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.OwnerApi(api_client)
    id = 'id_example' # str | 

    try:
        # Deny an open request. First write wins; the agent's next use files a fresh ask.
        api_response = api_instance.deny_request(id)
        print("The response of OwnerApi->deny_request:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->deny_request: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**|  | 

### Return type

[**ApprovalRequest**](ApprovalRequest.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | The denied request |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | not owner |  -  |
**404** | no such request in this org |  -  |
**409** | already resolved or expired |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **import_items**
> ImportResponse import_items(body, filename=filename)

One-shot 1Password .1pux or CSV onto origin. Secret in the file, never in the response. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.import_response import ImportResponse
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)

# The client must configure the authentication and authorization parameters
# in accordance with the API server security policy.
# Examples for each auth method are provided below, use the example that
# satisfies your auth use case.

# Configure Bearer authorization (JWT): bearerAuth
configuration = veil.Configuration(
    access_token = os.environ["BEARER_TOKEN"]
)

# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.OwnerApi(api_client)
    body = None # bytes | 
    filename = 'filename_example' # str | export.1pux or chrome.csv. Sniffed from bytes when empty. (optional)

    try:
        # One-shot 1Password .1pux or CSV onto origin. Secret in the file, never in the response. Not MCP.
        api_response = api_instance.import_items(body, filename=filename)
        print("The response of OwnerApi->import_items:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->import_items: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | **bytes**|  | 
 **filename** | **str**| export.1pux or chrome.csv. Sniffed from bytes when empty. | [optional] 

### Return type

[**ImportResponse**](ImportResponse.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/octet-stream, text/csv
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Created item names. No secrets. |  -  |
**400** | bad request |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | agent cannot import |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **list_agents**
> AgentsResponse list_agents()

Agents in this org. Ids only. Never secrets. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.agents_response import AgentsResponse
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)

# The client must configure the authentication and authorization parameters
# in accordance with the API server security policy.
# Examples for each auth method are provided below, use the example that
# satisfies your auth use case.

# Configure Bearer authorization (JWT): bearerAuth
configuration = veil.Configuration(
    access_token = os.environ["BEARER_TOKEN"]
)

# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.OwnerApi(api_client)

    try:
        # Agents in this org. Ids only. Never secrets. Not MCP.
        api_response = api_instance.list_agents()
        print("The response of OwnerApi->list_agents:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->list_agents: %s\n" % e)
```



### Parameters

This endpoint does not need any parameter.

### Return type

[**AgentsResponse**](AgentsResponse.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Agents |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | not owner |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **list_grants**
> GrantsResponse list_grants()

Grants in this org. No secrets. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.grants_response import GrantsResponse
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)

# The client must configure the authentication and authorization parameters
# in accordance with the API server security policy.
# Examples for each auth method are provided below, use the example that
# satisfies your auth use case.

# Configure Bearer authorization (JWT): bearerAuth
configuration = veil.Configuration(
    access_token = os.environ["BEARER_TOKEN"]
)

# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.OwnerApi(api_client)

    try:
        # Grants in this org. No secrets. Not MCP.
        api_response = api_instance.list_grants()
        print("The response of OwnerApi->list_grants:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->list_grants: %s\n" % e)
```



### Parameters

This endpoint does not need any parameter.

### Return type

[**GrantsResponse**](GrantsResponse.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Grants |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | not owner |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **list_requests**
> RequestsResponse list_requests(status=status)

Approval requests filed by level-1 agents. status=open lists only unexpired asks. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.requests_response import RequestsResponse
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)

# The client must configure the authentication and authorization parameters
# in accordance with the API server security policy.
# Examples for each auth method are provided below, use the example that
# satisfies your auth use case.

# Configure Bearer authorization (JWT): bearerAuth
configuration = veil.Configuration(
    access_token = os.environ["BEARER_TOKEN"]
)

# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.OwnerApi(api_client)
    status = open # str |  (optional) (default to open)

    try:
        # Approval requests filed by level-1 agents. status=open lists only unexpired asks. Not MCP.
        api_response = api_instance.list_requests(status=status)
        print("The response of OwnerApi->list_requests:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->list_requests: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **status** | **str**|  | [optional] [default to open]

### Return type

[**RequestsResponse**](RequestsResponse.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Requests |  -  |
**400** | bad status |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | not owner |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **list_sessions**
> SessionsResponse list_sessions()

Active sandbox sessions. Metadata only. Never the token. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.sessions_response import SessionsResponse
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)

# The client must configure the authentication and authorization parameters
# in accordance with the API server security policy.
# Examples for each auth method are provided below, use the example that
# satisfies your auth use case.

# Configure Bearer authorization (JWT): bearerAuth
configuration = veil.Configuration(
    access_token = os.environ["BEARER_TOKEN"]
)

# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.OwnerApi(api_client)

    try:
        # Active sandbox sessions. Metadata only. Never the token. Not MCP.
        api_response = api_instance.list_sessions()
        print("The response of OwnerApi->list_sessions:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->list_sessions: %s\n" % e)
```



### Parameters

This endpoint does not need any parameter.

### Return type

[**SessionsResponse**](SessionsResponse.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Sessions |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | not owner |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **revoke_agent**
> Agent revoke_agent(name)

Revoke an agent. Idempotent. Kills grants, sessions, and in-flight Use. Record stays for audit.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.agent import Agent
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)

# The client must configure the authentication and authorization parameters
# in accordance with the API server security policy.
# Examples for each auth method are provided below, use the example that
# satisfies your auth use case.

# Configure Bearer authorization (JWT): bearerAuth
configuration = veil.Configuration(
    access_token = os.environ["BEARER_TOKEN"]
)

# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.OwnerApi(api_client)
    name = 'name_example' # str | 

    try:
        # Revoke an agent. Idempotent. Kills grants, sessions, and in-flight Use. Record stays for audit.
        api_response = api_instance.revoke_agent(name)
        print("The response of OwnerApi->revoke_agent:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->revoke_agent: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **name** | **str**|  | 

### Return type

[**Agent**](Agent.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Revoked agent. No token or secret. |  -  |
**400** | bad request |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | not owner |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **stream_requests**
> str stream_requests()

Server-sent events feed of approval-request changes for the owner org. Each event is an empty tick — refetch GET /v1/requests for the authoritative rows. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)

# The client must configure the authentication and authorization parameters
# in accordance with the API server security policy.
# Examples for each auth method are provided below, use the example that
# satisfies your auth use case.

# Configure Bearer authorization (JWT): bearerAuth
configuration = veil.Configuration(
    access_token = os.environ["BEARER_TOKEN"]
)

# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.OwnerApi(api_client)

    try:
        # Server-sent events feed of approval-request changes for the owner org. Each event is an empty tick — refetch GET /v1/requests for the authoritative rows. Not MCP.
        api_response = api_instance.stream_requests()
        print("The response of OwnerApi->stream_requests:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->stream_requests: %s\n" % e)
```



### Parameters

This endpoint does not need any parameter.

### Return type

**str**

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: text/event-stream

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | text/event-stream; stays open until the client disconnects |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | not owner |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **update_item**
> Item update_item(name, update_item_request)

Replace URIs, tags, and fill username. No secret.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.item import Item
from veil.models.update_item_request import UpdateItemRequest
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)

# The client must configure the authentication and authorization parameters
# in accordance with the API server security policy.
# Examples for each auth method are provided below, use the example that
# satisfies your auth use case.

# Configure Bearer authorization (JWT): bearerAuth
configuration = veil.Configuration(
    access_token = os.environ["BEARER_TOKEN"]
)

# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.OwnerApi(api_client)
    name = 'name_example' # str | 
    update_item_request = veil.UpdateItemRequest() # UpdateItemRequest | 

    try:
        # Replace URIs, tags, and fill username. No secret.
        api_response = api_instance.update_item(name, update_item_request)
        print("The response of OwnerApi->update_item:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->update_item: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **name** | **str**|  | 
 **update_item_request** | [**UpdateItemRequest**](UpdateItemRequest.md)|  | 

### Return type

[**Item**](Item.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Item metadata |  -  |
**400** | bad request |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | agent cannot update items |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

