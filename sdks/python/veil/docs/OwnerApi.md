# veil.OwnerApi

All URIs are relative to *https://veil.nyc*

Method | HTTP request | Description
------------- | ------------- | -------------
[**add_group_member**](OwnerApi.md#add_group_member) | **POST** /v1/groups/{name}/members | Add an agent or human to the group. Not MCP.
[**approve_request**](OwnerApi.md#approve_request) | **POST** /v1/requests/{id}/approve | Approve an open request — unlocks its level-1 grant for ttl. First write wins.
[**archive_item**](OwnerApi.md#archive_item) | **POST** /v1/items/{name}/archive | Hide from Use and list. History stays.
[**bind_workload**](OwnerApi.md#bind_workload) | **POST** /v1/agents/{name}/bind | Bind an existing agent to an external OIDC issuer, subject, and audience (workload identity federation). Owner-only — binding an auth method to an agent is a grant-adjacent write.
[**create_agent**](OwnerApi.md#create_agent) | **POST** /v1/agents | Register an agent principal. Not a Hydra secret. Not MCP.
[**create_grant**](OwnerApi.md#create_grant) | **POST** /v1/grants | Grant an agent or a Kratos human Use on an item. Same grant object. Not MCP. Not a family vault.
[**create_group**](OwnerApi.md#create_group) | **POST** /v1/groups | Create an org group. Not MCP.
[**create_item**](OwnerApi.md#create_item) | **POST** /v1/items | Create an item. Secret is in the request over TLS. Never in the response. Not MCP.
[**create_session**](OwnerApi.md#create_session) | **POST** /v1/sessions | Mint a short-lived Use lease onto an existing agent. Token is in this response once. Sandbox gets the session file, not the agent JWT. Default 15m, max 1h. Not MCP.
[**delete_item**](OwnerApi.md#delete_item) | **DELETE** /v1/items/{name} | Remove the item and its grants. Not MCP.
[**deny_request**](OwnerApi.md#deny_request) | **POST** /v1/requests/{id}/deny | Deny an open request. First write wins; the agent&#39;s next use files a fresh ask.
[**import_items**](OwnerApi.md#import_items) | **POST** /v1/import | One-shot 1Password .1pux or CSV onto origin. Secret in the file, never in the response. Not MCP.
[**list_agent_workloads**](OwnerApi.md#list_agent_workloads) | **GET** /v1/agents/{name}/workloads | List bound OIDC workload policies for an agent. Owner-only.
[**list_agents**](OwnerApi.md#list_agents) | **GET** /v1/agents | Agents in this org. Ids only. Never secrets. Not MCP.
[**list_grants**](OwnerApi.md#list_grants) | **GET** /v1/grants | Grants in this org. No secrets. Not MCP.
[**list_group_members**](OwnerApi.md#list_group_members) | **GET** /v1/groups/{name}/members | Members of the group — agent or human edges. Not MCP.
[**list_groups**](OwnerApi.md#list_groups) | **GET** /v1/groups | Org groups. A grant to a group is the shared vault. Not MCP.
[**list_requests**](OwnerApi.md#list_requests) | **GET** /v1/requests | Approval requests filed by level-1 agents. status&#x3D;open lists only unexpired asks. Not MCP.
[**list_sessions**](OwnerApi.md#list_sessions) | **GET** /v1/sessions | Active sandbox sessions. Metadata only. Never the token. Not MCP.
[**remove_group_member**](OwnerApi.md#remove_group_member) | **DELETE** /v1/groups/{name}/members/{kind}/{id} | Remove a member from the group. Not MCP.
[**revoke_agent**](OwnerApi.md#revoke_agent) | **POST** /v1/agents/{name}/revoke | Revoke an agent. Idempotent. Kills grants, sessions, and in-flight Use. Record stays for audit.
[**rotate_item**](OwnerApi.md#rotate_item) | **POST** /v1/items/{name}/rotate | Rotate an item&#39;s credential via the provider&#39;s key-management API. The new secret is verified and sealed in place — item id and grants are untouched, agents keep calling Use. Owner-only. 422 when the provider has no rotator.
[**stream_requests**](OwnerApi.md#stream_requests) | **GET** /v1/requests/stream | Server-sent events feed of approval-request changes for the owner org. Each event is an empty tick — refetch GET /v1/requests for the authoritative rows. Not MCP.
[**unbind_workload**](OwnerApi.md#unbind_workload) | **DELETE** /v1/agents/{name}/workloads | Remove an OIDC workload binding from an agent. Owner-only.
[**update_item**](OwnerApi.md#update_item) | **PATCH** /v1/items/{name} | Replace URIs, tags, and fill username. No secret.
[**verify_issuer**](OwnerApi.md#verify_issuer) | **POST** /v1/workloads/verify-issuer | Dry-run OIDC discovery against an issuer URL before saving a workload binding. Owner-only — never mounted on /mcp.


# **add_group_member**
> add_group_member(name, add_group_member_request)

Add an agent or human to the group. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.add_group_member_request import AddGroupMemberRequest
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
    add_group_member_request = veil.AddGroupMemberRequest() # AddGroupMemberRequest | 

    try:
        # Add an agent or human to the group. Not MCP.
        api_instance.add_group_member(name, add_group_member_request)
    except Exception as e:
        print("Exception when calling OwnerApi->add_group_member: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **name** | **str**|  | 
 **add_group_member_request** | [**AddGroupMemberRequest**](AddGroupMemberRequest.md)|  | 

### Return type

void (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: Not defined

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | added |  -  |
**400** | bad request |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | not owner |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

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

# **bind_workload**
> WorkloadView bind_workload(name, bind_workload_request)

Bind an existing agent to an external OIDC issuer, subject, and audience (workload identity federation). Owner-only — binding an auth method to an agent is a grant-adjacent write.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.bind_workload_request import BindWorkloadRequest
from veil.models.workload_view import WorkloadView
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
    bind_workload_request = veil.BindWorkloadRequest() # BindWorkloadRequest | 

    try:
        # Bind an existing agent to an external OIDC issuer, subject, and audience (workload identity federation). Owner-only — binding an auth method to an agent is a grant-adjacent write.
        api_response = api_instance.bind_workload(name, bind_workload_request)
        print("The response of OwnerApi->bind_workload:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->bind_workload: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **name** | **str**|  | 
 **bind_workload_request** | [**BindWorkloadRequest**](BindWorkloadRequest.md)|  | 

### Return type

[**WorkloadView**](WorkloadView.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Workload binding |  -  |
**400** | bind failed |  -  |
**401** | missing or invalid bearer |  -  |
**403** | not the owner |  -  |

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

# **create_group**
> Group create_group(create_group_request)

Create an org group. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.create_group_request import CreateGroupRequest
from veil.models.group import Group
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
    create_group_request = veil.CreateGroupRequest() # CreateGroupRequest | 

    try:
        # Create an org group. Not MCP.
        api_response = api_instance.create_group(create_group_request)
        print("The response of OwnerApi->create_group:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->create_group: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **create_group_request** | [**CreateGroupRequest**](CreateGroupRequest.md)|  | 

### Return type

[**Group**](Group.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Group |  -  |
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

# **list_agent_workloads**
> WorkloadsResponse list_agent_workloads(name)

List bound OIDC workload policies for an agent. Owner-only.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.workloads_response import WorkloadsResponse
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
        # List bound OIDC workload policies for an agent. Owner-only.
        api_response = api_instance.list_agent_workloads(name)
        print("The response of OwnerApi->list_agent_workloads:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->list_agent_workloads: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **name** | **str**|  | 

### Return type

[**WorkloadsResponse**](WorkloadsResponse.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Workload bindings |  -  |
**400** | list workloads failed |  -  |
**401** | missing or invalid bearer |  -  |
**403** | not the owner |  -  |

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

# **list_group_members**
> GroupMembersResponse list_group_members(name)

Members of the group — agent or human edges. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.group_members_response import GroupMembersResponse
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
        # Members of the group — agent or human edges. Not MCP.
        api_response = api_instance.list_group_members(name)
        print("The response of OwnerApi->list_group_members:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->list_group_members: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **name** | **str**|  | 

### Return type

[**GroupMembersResponse**](GroupMembersResponse.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Members |  -  |
**400** | unknown group |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | not owner |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **list_groups**
> GroupsResponse list_groups()

Org groups. A grant to a group is the shared vault. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.groups_response import GroupsResponse
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
        # Org groups. A grant to a group is the shared vault. Not MCP.
        api_response = api_instance.list_groups()
        print("The response of OwnerApi->list_groups:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->list_groups: %s\n" % e)
```



### Parameters

This endpoint does not need any parameter.

### Return type

[**GroupsResponse**](GroupsResponse.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Groups |  -  |
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

# **remove_group_member**
> remove_group_member(name, kind, id)

Remove a member from the group. Not MCP.

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
    kind = 'kind_example' # str | 
    id = 'id_example' # str | 

    try:
        # Remove a member from the group. Not MCP.
        api_instance.remove_group_member(name, kind, id)
    except Exception as e:
        print("Exception when calling OwnerApi->remove_group_member: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **name** | **str**|  | 
 **kind** | **str**|  | 
 **id** | **str**|  | 

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
**200** | removed |  -  |
**400** | bad request |  -  |
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

# **rotate_item**
> RotateItem200Response rotate_item(name, rotate_item_request=rotate_item_request)

Rotate an item's credential via the provider's key-management API. The new secret is verified and sealed in place — item id and grants are untouched, agents keep calling Use. Owner-only. 422 when the provider has no rotator.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.rotate_item200_response import RotateItem200Response
from veil.models.rotate_item_request import RotateItemRequest
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
    rotate_item_request = veil.RotateItemRequest() # RotateItemRequest |  (optional)

    try:
        # Rotate an item's credential via the provider's key-management API. The new secret is verified and sealed in place — item id and grants are untouched, agents keep calling Use. Owner-only. 422 when the provider has no rotator.
        api_response = api_instance.rotate_item(name, rotate_item_request=rotate_item_request)
        print("The response of OwnerApi->rotate_item:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->rotate_item: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **name** | **str**|  | 
 **rotate_item_request** | [**RotateItemRequest**](RotateItemRequest.md)|  | [optional] 

### Return type

[**RotateItem200Response**](RotateItem200Response.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Rotated |  -  |
**400** | rotate failed |  -  |
**401** | missing or invalid bearer |  -  |
**403** | not the owner |  -  |
**422** | provider has no key-management API |  -  |

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

# **unbind_workload**
> unbind_workload(name, issuer, subject)

Remove an OIDC workload binding from an agent. Owner-only.

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
    issuer = 'issuer_example' # str | 
    subject = 'subject_example' # str | 

    try:
        # Remove an OIDC workload binding from an agent. Owner-only.
        api_instance.unbind_workload(name, issuer, subject)
    except Exception as e:
        print("Exception when calling OwnerApi->unbind_workload: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **name** | **str**|  | 
 **issuer** | **str**|  | 
 **subject** | **str**|  | 

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
**204** | Unbound |  -  |
**400** | unbind failed |  -  |
**401** | missing or invalid bearer |  -  |
**403** | not the owner |  -  |

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

# **verify_issuer**
> VerifyIssuerResponse verify_issuer(verify_issuer_request)

Dry-run OIDC discovery against an issuer URL before saving a workload binding. Owner-only — never mounted on /mcp.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.verify_issuer_request import VerifyIssuerRequest
from veil.models.verify_issuer_response import VerifyIssuerResponse
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
    verify_issuer_request = veil.VerifyIssuerRequest() # VerifyIssuerRequest | 

    try:
        # Dry-run OIDC discovery against an issuer URL before saving a workload binding. Owner-only — never mounted on /mcp.
        api_response = api_instance.verify_issuer(verify_issuer_request)
        print("The response of OwnerApi->verify_issuer:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling OwnerApi->verify_issuer: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **verify_issuer_request** | [**VerifyIssuerRequest**](VerifyIssuerRequest.md)|  | 

### Return type

[**VerifyIssuerResponse**](VerifyIssuerResponse.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Verification result |  -  |
**400** | bad request |  -  |
**401** | missing or invalid bearer |  -  |
**403** | not the owner |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

