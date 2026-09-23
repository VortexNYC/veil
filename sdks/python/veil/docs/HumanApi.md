# veil.HumanApi

All URIs are relative to *https://veil.nyc*

Method | HTTP request | Description
------------- | ------------- | -------------
[**create_invite**](HumanApi.md#create_invite) | **POST** /v1/invites | Private-alpha invite. Owner-authenticated — the bearer is a verified human ID token of a provisioned human. Creates the Kratos identity plus recovery link under the inviter&#39;s org and emails the setup link via the mail worker. recovery_url only appears when mail is not configured (local dev). Idempotent for re-invites. Not MCP.
[**delete_me**](HumanApi.md#delete_me) | **DELETE** /v1/me | Leave the org: drops the member (and owner, when present) tuples plus the humans row — the token resolves nothing afterward. A sole owner is refused: promote a member or delete the org. Not MCP.
[**delete_org**](HumanApi.md#delete_org) | **DELETE** /v1/org | Teardown. Owner-only. Deletes every Keto tuple for the org, then purges every vault row (items, grants, sessions, agents, humans, keys) in one transaction. Audit rows are kept — teardown must not erase the forensic record. Not MCP.
[**demote_owner**](HumanApi.md#demote_owner) | **DELETE** /v1/members/{id}/owner | Strip the owners tuple from a co-owner. Owner-only. The last owner cannot be demoted — the org would have no administrator. Not MCP.
[**promote_owner**](HumanApi.md#promote_owner) | **POST** /v1/members/{id}/owner | Grant an existing member the owners tuple. Owner-only. Not MCP.
[**provision**](HumanApi.md#provision) | **POST** /v1/provision | Signup provisioning. Subject-authenticated — the bearer is a verified human ID token, not a member yet. Creates the org, seals the org master under the deployment KEK, plants the humans row, and writes the Keto owner/member tuples plus the Kratos organization_id stamp. Idempotent; safe to retry. Not MCP.
[**remove_member**](HumanApi.md#remove_member) | **DELETE** /v1/members/{id} | Offboard a member. Owner-only — drops the Keto member tuple and the humans row; the removed member&#39;s token resolves nothing from that call on. Owners cannot be removed this way (demote first). Not MCP.


# **create_invite**
> InviteResponse create_invite(invite_request)

Private-alpha invite. Owner-authenticated — the bearer is a verified human ID token of a provisioned human. Creates the Kratos identity plus recovery link under the inviter's org and emails the setup link via the mail worker. recovery_url only appears when mail is not configured (local dev). Idempotent for re-invites. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.invite_request import InviteRequest
from veil.models.invite_response import InviteResponse
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
    api_instance = veil.HumanApi(api_client)
    invite_request = veil.InviteRequest() # InviteRequest | 

    try:
        # Private-alpha invite. Owner-authenticated — the bearer is a verified human ID token of a provisioned human. Creates the Kratos identity plus recovery link under the inviter's org and emails the setup link via the mail worker. recovery_url only appears when mail is not configured (local dev). Idempotent for re-invites. Not MCP.
        api_response = api_instance.create_invite(invite_request)
        print("The response of HumanApi->create_invite:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling HumanApi->create_invite: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **invite_request** | [**InviteRequest**](InviteRequest.md)|  | 

### Return type

[**InviteResponse**](InviteResponse.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Invite result |  -  |
**400** | missing email |  -  |
**401** | missing or invalid human token |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_me**
> DeleteMe200Response delete_me()

Leave the org: drops the member (and owner, when present) tuples plus the humans row — the token resolves nothing afterward. A sole owner is refused: promote a member or delete the org. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.delete_me200_response import DeleteMe200Response
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
    api_instance = veil.HumanApi(api_client)

    try:
        # Leave the org: drops the member (and owner, when present) tuples plus the humans row — the token resolves nothing afterward. A sole owner is refused: promote a member or delete the org. Not MCP.
        api_response = api_instance.delete_me()
        print("The response of HumanApi->delete_me:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling HumanApi->delete_me: %s\n" % e)
```



### Parameters

This endpoint does not need any parameter.

### Return type

[**DeleteMe200Response**](DeleteMe200Response.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Account removed from org |  -  |
**400** | sole-owner leave refused |  -  |
**401** | missing or invalid human token |  -  |
**403** | not a provisioned human |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_org**
> DeleteOrg200Response delete_org()

Teardown. Owner-only. Deletes every Keto tuple for the org, then purges every vault row (items, grants, sessions, agents, humans, keys) in one transaction. Audit rows are kept — teardown must not erase the forensic record. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.delete_org200_response import DeleteOrg200Response
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
    api_instance = veil.HumanApi(api_client)

    try:
        # Teardown. Owner-only. Deletes every Keto tuple for the org, then purges every vault row (items, grants, sessions, agents, humans, keys) in one transaction. Audit rows are kept — teardown must not erase the forensic record. Not MCP.
        api_response = api_instance.delete_org()
        print("The response of HumanApi->delete_org:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling HumanApi->delete_org: %s\n" % e)
```



### Parameters

This endpoint does not need any parameter.

### Return type

[**DeleteOrg200Response**](DeleteOrg200Response.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Org purged |  -  |
**401** | missing or invalid human token |  -  |
**403** | caller is a member, not an owner |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **demote_owner**
> PromoteOwner200Response demote_owner(id)

Strip the owners tuple from a co-owner. Owner-only. The last owner cannot be demoted — the org would have no administrator. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.promote_owner200_response import PromoteOwner200Response
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
    api_instance = veil.HumanApi(api_client)
    id = 'id_example' # str | Kratos identity id of the owner

    try:
        # Strip the owners tuple from a co-owner. Owner-only. The last owner cannot be demoted — the org would have no administrator. Not MCP.
        api_response = api_instance.demote_owner(id)
        print("The response of HumanApi->demote_owner:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling HumanApi->demote_owner: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**| Kratos identity id of the owner | 

### Return type

[**PromoteOwner200Response**](PromoteOwner200Response.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Owner demoted |  -  |
**400** | last-owner demotion refused |  -  |
**401** | missing or invalid human token |  -  |
**403** | caller is a member, not an owner |  -  |
**404** | not an owner of this org |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **promote_owner**
> PromoteOwner200Response promote_owner(id)

Grant an existing member the owners tuple. Owner-only. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.promote_owner200_response import PromoteOwner200Response
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
    api_instance = veil.HumanApi(api_client)
    id = 'id_example' # str | Kratos identity id of the member

    try:
        # Grant an existing member the owners tuple. Owner-only. Not MCP.
        api_response = api_instance.promote_owner(id)
        print("The response of HumanApi->promote_owner:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling HumanApi->promote_owner: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**| Kratos identity id of the member | 

### Return type

[**PromoteOwner200Response**](PromoteOwner200Response.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Member promoted |  -  |
**401** | missing or invalid human token |  -  |
**403** | caller is a member, not an owner |  -  |
**404** | not a member of this org |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **provision**
> ProvisionResponse provision()

Signup provisioning. Subject-authenticated — the bearer is a verified human ID token, not a member yet. Creates the org, seals the org master under the deployment KEK, plants the humans row, and writes the Keto owner/member tuples plus the Kratos organization_id stamp. Idempotent; safe to retry. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.provision_response import ProvisionResponse
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
    api_instance = veil.HumanApi(api_client)

    try:
        # Signup provisioning. Subject-authenticated — the bearer is a verified human ID token, not a member yet. Creates the org, seals the org master under the deployment KEK, plants the humans row, and writes the Keto owner/member tuples plus the Kratos organization_id stamp. Idempotent; safe to retry. Not MCP.
        api_response = api_instance.provision()
        print("The response of HumanApi->provision:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling HumanApi->provision: %s\n" % e)
```



### Parameters

This endpoint does not need any parameter.

### Return type

[**ProvisionResponse**](ProvisionResponse.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Provisioned principal |  -  |
**401** | missing or invalid human token |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **remove_member**
> RemoveMember200Response remove_member(id)

Offboard a member. Owner-only — drops the Keto member tuple and the humans row; the removed member's token resolves nothing from that call on. Owners cannot be removed this way (demote first). Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.remove_member200_response import RemoveMember200Response
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
    api_instance = veil.HumanApi(api_client)
    id = 'id_example' # str | Kratos identity id of the member

    try:
        # Offboard a member. Owner-only — drops the Keto member tuple and the humans row; the removed member's token resolves nothing from that call on. Owners cannot be removed this way (demote first). Not MCP.
        api_response = api_instance.remove_member(id)
        print("The response of HumanApi->remove_member:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling HumanApi->remove_member: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**| Kratos identity id of the member | 

### Return type

[**RemoveMember200Response**](RemoveMember200Response.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Member removed |  -  |
**401** | missing or invalid human token |  -  |
**403** | caller is a member, not an owner |  -  |
**404** | not a member of this org |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

