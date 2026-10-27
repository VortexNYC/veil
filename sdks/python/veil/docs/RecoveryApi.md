# veil.RecoveryApi

All URIs are relative to *https://veil.nyc*

Method | HTTP request | Description
------------- | ------------- | -------------
[**complete_recovery**](RecoveryApi.md#complete_recovery) | **POST** /v1/recovery/{id}/complete | The confirmer&#39;s commit (owner, aal2): rotates the org master, re-wraps every member share and owner DEK, moves the member&#39;s share to the staged pubkey at epoch+1, and fires the Kratos MFA reset when reset_mfa was filed. Multi-owner orgs enforce two-person integrity. proof_hmac bypasses the sole-owner timelock. The MFA reset code never rides the response.
[**deny_recovery**](RecoveryApi.md#deny_recovery) | **POST** /v1/recovery/{id}/deny | Resolve an open request: an owner denies (aal2); the initiator or target member cancels at any aal. First write wins.
[**enroll_recovery**](RecoveryApi.md#enroll_recovery) | **POST** /v1/recovery/enroll | Initial recovery-share enrollment (aal2). Insert-if-absent: a member&#39;s pubkey never rotates outside the ceremony, so an existing share is a 409.
[**get_recovery**](RecoveryApi.md#get_recovery) | **GET** /v1/recovery/{id} | One request. Owners (aal2) get the confirmer view — human_email, current_epoch, confirmer_wrapped_master; the initiator or target member gets the bare row.
[**get_recovery_share**](RecoveryApi.md#get_recovery_share) | **GET** /v1/recovery/share | The caller&#39;s own recovery-share state: enrolled flag, pubkey, and wrapped_master (ciphertext only — never the plaintext master).
[**list_recoveries**](RecoveryApi.md#list_recoveries) | **GET** /v1/recovery | Owner-only (aal2) list of org recovery requests filtered by status.
[**start_recovery**](RecoveryApi.md#start_recovery) | **POST** /v1/recovery/start | Files (or returns the existing open) recovery request. Self-start needs no aal2 — a locked-out member may hold only aal1. Owner-initiated starts (human_id set) require aal2 + owner. One open request per member: a refile returns the live row with created&#x3D;false and suppresses duplicate notifications.


# **complete_recovery**
> RecoveryRequest complete_recovery(id, complete_recovery_request=complete_recovery_request)

The confirmer's commit (owner, aal2): rotates the org master, re-wraps every member share and owner DEK, moves the member's share to the staged pubkey at epoch+1, and fires the Kratos MFA reset when reset_mfa was filed. Multi-owner orgs enforce two-person integrity. proof_hmac bypasses the sole-owner timelock. The MFA reset code never rides the response.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.complete_recovery_request import CompleteRecoveryRequest
from veil.models.recovery_request import RecoveryRequest
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
    api_instance = veil.RecoveryApi(api_client)
    id = 'id_example' # str | 
    complete_recovery_request = veil.CompleteRecoveryRequest() # CompleteRecoveryRequest |  (optional)

    try:
        # The confirmer's commit (owner, aal2): rotates the org master, re-wraps every member share and owner DEK, moves the member's share to the staged pubkey at epoch+1, and fires the Kratos MFA reset when reset_mfa was filed. Multi-owner orgs enforce two-person integrity. proof_hmac bypasses the sole-owner timelock. The MFA reset code never rides the response.
        api_response = api_instance.complete_recovery(id, complete_recovery_request=complete_recovery_request)
        print("The response of RecoveryApi->complete_recovery:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling RecoveryApi->complete_recovery: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**|  | 
 **complete_recovery_request** | [**CompleteRecoveryRequest**](CompleteRecoveryRequest.md)|  | [optional] 

### Return type

[**RecoveryRequest**](RecoveryRequest.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | The completed request |  -  |
**400** | malformed proof_hmac |  -  |
**401** | missing or invalid human token |  -  |
**403** | not owner, aal1, or two-person integrity violation |  -  |
**404** | no such request, or target was offboarded |  -  |
**409** | already resolved or expired |  -  |
**423** | still inside the unlock_at timelock |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **deny_recovery**
> RecoveryRequest deny_recovery(id)

Resolve an open request: an owner denies (aal2); the initiator or target member cancels at any aal. First write wins.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.recovery_request import RecoveryRequest
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
    api_instance = veil.RecoveryApi(api_client)
    id = 'id_example' # str | 

    try:
        # Resolve an open request: an owner denies (aal2); the initiator or target member cancels at any aal. First write wins.
        api_response = api_instance.deny_recovery(id)
        print("The response of RecoveryApi->deny_recovery:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling RecoveryApi->deny_recovery: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**|  | 

### Return type

[**RecoveryRequest**](RecoveryRequest.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | The resolved request |  -  |
**401** | missing or invalid human token |  -  |
**403** | not owner, initiator, or target — or aal1 owner |  -  |
**404** | no such request in this org |  -  |
**409** | already resolved |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **enroll_recovery**
> RecoveryShare enroll_recovery(enroll_request)

Initial recovery-share enrollment (aal2). Insert-if-absent: a member's pubkey never rotates outside the ceremony, so an existing share is a 409.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.enroll_request import EnrollRequest
from veil.models.recovery_share import RecoveryShare
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
    api_instance = veil.RecoveryApi(api_client)
    enroll_request = veil.EnrollRequest() # EnrollRequest | 

    try:
        # Initial recovery-share enrollment (aal2). Insert-if-absent: a member's pubkey never rotates outside the ceremony, so an existing share is a 409.
        api_response = api_instance.enroll_recovery(enroll_request)
        print("The response of RecoveryApi->enroll_recovery:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling RecoveryApi->enroll_recovery: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **enroll_request** | [**EnrollRequest**](EnrollRequest.md)|  | 

### Return type

[**RecoveryShare**](RecoveryShare.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | The planted share |  -  |
**400** | missing or malformed pubkey |  -  |
**401** | missing or invalid human token |  -  |
**403** | aal1 token — enrollment requires a second factor |  -  |
**409** | already enrolled |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_recovery**
> RecoveryRequest get_recovery(id)

One request. Owners (aal2) get the confirmer view — human_email, current_epoch, confirmer_wrapped_master; the initiator or target member gets the bare row.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.recovery_request import RecoveryRequest
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
    api_instance = veil.RecoveryApi(api_client)
    id = 'id_example' # str | 

    try:
        # One request. Owners (aal2) get the confirmer view — human_email, current_epoch, confirmer_wrapped_master; the initiator or target member gets the bare row.
        api_response = api_instance.get_recovery(id)
        print("The response of RecoveryApi->get_recovery:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling RecoveryApi->get_recovery: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**|  | 

### Return type

[**RecoveryRequest**](RecoveryRequest.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | The request |  -  |
**401** | missing or invalid human token |  -  |
**403** | not owner, initiator, or target — or aal1 owner |  -  |
**404** | no such request in this org |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_recovery_share**
> RecoveryShare get_recovery_share()

The caller's own recovery-share state: enrolled flag, pubkey, and wrapped_master (ciphertext only — never the plaintext master).

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.recovery_share import RecoveryShare
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
    api_instance = veil.RecoveryApi(api_client)

    try:
        # The caller's own recovery-share state: enrolled flag, pubkey, and wrapped_master (ciphertext only — never the plaintext master).
        api_response = api_instance.get_recovery_share()
        print("The response of RecoveryApi->get_recovery_share:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling RecoveryApi->get_recovery_share: %s\n" % e)
```



### Parameters

This endpoint does not need any parameter.

### Return type

[**RecoveryShare**](RecoveryShare.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Share state |  -  |
**401** | missing or invalid human token |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **list_recoveries**
> RecoveriesResponse list_recoveries(status=status)

Owner-only (aal2) list of org recovery requests filtered by status.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.recoveries_response import RecoveriesResponse
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
    api_instance = veil.RecoveryApi(api_client)
    status = open # str |  (optional) (default to open)

    try:
        # Owner-only (aal2) list of org recovery requests filtered by status.
        api_response = api_instance.list_recoveries(status=status)
        print("The response of RecoveryApi->list_recoveries:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling RecoveryApi->list_recoveries: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **status** | **str**|  | [optional] [default to open]

### Return type

[**RecoveriesResponse**](RecoveriesResponse.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | The org&#39;s requests |  -  |
**400** | bad status |  -  |
**401** | missing or invalid human token |  -  |
**403** | not owner or aal1 |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **start_recovery**
> StartRecoveryResponse start_recovery(start_recovery_request)

Files (or returns the existing open) recovery request. Self-start needs no aal2 — a locked-out member may hold only aal1. Owner-initiated starts (human_id set) require aal2 + owner. One open request per member: a refile returns the live row with created=false and suppresses duplicate notifications.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.start_recovery_request import StartRecoveryRequest
from veil.models.start_recovery_response import StartRecoveryResponse
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
    api_instance = veil.RecoveryApi(api_client)
    start_recovery_request = veil.StartRecoveryRequest() # StartRecoveryRequest | 

    try:
        # Files (or returns the existing open) recovery request. Self-start needs no aal2 — a locked-out member may hold only aal1. Owner-initiated starts (human_id set) require aal2 + owner. One open request per member: a refile returns the live row with created=false and suppresses duplicate notifications.
        api_response = api_instance.start_recovery(start_recovery_request)
        print("The response of RecoveryApi->start_recovery:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling RecoveryApi->start_recovery: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **start_recovery_request** | [**StartRecoveryRequest**](StartRecoveryRequest.md)|  | 

### Return type

[**StartRecoveryResponse**](StartRecoveryResponse.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | The request (created&#x3D;false when deduped) |  -  |
**400** | malformed new_pubkey or a pubkey already enrolled on a share |  -  |
**401** | missing or invalid human token |  -  |
**403** | non-owner naming another human, or aal1 owner-initiated start |  -  |
**404** | named human is not an org member |  -  |
**429** | recovery start rate limit |  -  |
**502** | identity plane unavailable |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

