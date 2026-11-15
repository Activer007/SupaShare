// This file contains JS code to connect to the API
// All files named admin_*.js will be merged together and minimised by calling
// go generate ./...


const storedTokens = new Map();

async function apiFailure(response) {
    const text = await response.text();
    let data;
    try { data = JSON.parse(text); } catch { /* retain non-JSON diagnostics */ }
    const fallback = t('api.requestFailed', {status: response.status});
    const message = data && (data.ErrorCode !== undefined || data.ErrorMessage)
        ? apiErrorMessage(data) : (text || fallback);
    const error = new Error(message);
    error.status = response.status;
    if (data) { error.code = data.ErrorCode; error.raw = data; }
    return error;
}

async function getToken(permission, forceRenewal) {
    const apiUrl = './auth/token';

    if (!forceRenewal) {
        if (!storedTokens.has(permission)) {
            return getToken(permission, true);
        }
        let token = storedTokens.get(permission);
        if (token.expiry - (Date.now() / 1000) < 60) {
            return getToken(permission, true);
        }
        return token.key;
    }
    const requestOptions = {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'permission': permission

        },
    };
    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
        const data = await response.json();
        if (!data.hasOwnProperty("key")) {
            throw new Error(t("api.invalidToken"));
        }
        storedTokens.set(permission, {
            key: data.key,
            expiry: data.expiry
        });
        return data.key;
    } catch (error) {
        console.error("Error in getToken:", error);
        throw error;
    }
}

// /auth

async function apiAuthModify(apiKey, permission, modifier) {
    const apiUrl = './api/auth/modify';
    const reqPerm = 'PERM_API_MOD';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'targetKey': apiKey,
            'permission': permission,
            'permissionModifier': modifier

        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
    } catch (error) {
        console.error("Error in apiAuthModify:", error);
        throw error;
    }
}


async function apiAuthFriendlyName(apiKey, newName) {
    const apiUrl = './api/auth/friendlyname';
    const reqPerm = 'PERM_API_MOD';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'PUT',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'targetKey': apiKey,
            'friendlyName': encodeHeaderText(newName)

        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
    } catch (error) {
        console.error("Error in apiAuthModify:", error);
        throw error;
    }
}


async function apiAuthDelete(apiKey) {
    const apiUrl = './api/auth/delete';
    const reqPerm = 'PERM_API_MOD';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'targetKey': apiKey,
        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
    } catch (error) {
        console.error("Error in apiAuthDelete:", error);
        throw error;
    }
}


async function apiAuthCreate() {
    const apiUrl = './api/auth/create';
    const reqPerm = 'PERM_API_MOD';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'basicPermissions': 'true',
            'friendlyName': encodeHeaderText(t('api.unnamed'))
        },
    };


    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
        const data = await response.json();
        return data;
    } catch (error) {
        console.error("Error in apiAuthCreate:", error);
        throw error;
    }
}



// /chunk


async function apiChunkComplete(uuid, filename, filesize, realsize, contenttype, allowedDownloads, expiryDays, password, isE2E, nonblocking) {
    const apiUrl = './api/chunk/complete';
    const reqPerm = 'PERM_UPLOAD';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'uuid': uuid,
            'filename': 'base64:' + Base64.encode(filename),
            'filesize': filesize,
            'realsize': realsize,
            'contenttype': contenttype,
            'allowedDownloads': allowedDownloads,
            'expiryDays': expiryDays,
            'password': password,
            'isE2E': isE2E,
            'nonblocking': nonblocking
        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
        const data = await response.json();
        return data;
    } catch (error) {
        console.error("Error in apiChunkComplete:", error);
        throw error;
    }
}


// /files


async function apiFilesReplace(id, newId) {
    const apiUrl = './api/files/replace';
    const reqPerm = 'PERM_REPLACE';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'PUT',
        headers: {
            'Content-Type': 'application/json',
            'id': id,
            'apikey': token,
            'idNewContent': newId,
            'deleteNewFile': false
        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
        const data = await response.json();
        return data;
    } catch (error) {
        console.error("Error in apiFilesReplace:", error);
        throw error;
    }
}

async function apiCollectionFileShare(fileId) {
    const token = await getToken('PERM_UPLOAD', false);
    const response = await fetch('./api/files/duplicate', {
        method: 'POST',
        headers: {'Content-Type': 'application/json', apikey: token, id: fileId, asShare: 'true'}
    });
    if (!response.ok) throw await apiFailure(response);
    return response.json();
}

// Sharing feedback uses server-owned categories, never raw service diagnostics.
function shareFailureMessage(error) {
    const code = error?.code;
    const status = error?.status;
    if (code === 21) return t('sharing.errorQuota');
    if ([2, 3, 6].includes(code) || status === 401 || status === 403) return t('sharing.errorPermission');
    if (code === 5 || status === 404 || status === 409) return t('sharing.errorUnavailable');
    if ([16, 20, 22].includes(code) || status === 429 || status === 503) return t('sharing.errorBusy');
    if (error?.name === 'TypeError' && !status) return t('sharing.errorNetwork');
    return t('sharing.errorGeneric');
}

async function apiFilesListById(fileId) {
    const apiUrl = './api/files/list/' + fileId;
    const reqPerm = 'PERM_VIEW';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'GET',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token
        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
        const data = await response.json();
        return data;
    } catch (error) {
        console.error("Error in apiFilesListById:", error);
        throw error;
    }
}


async function apiFilesListDownloadSingle(fileId) {
    const apiUrl = './api/files/download/' + fileId;
    const reqPerm = 'PERM_DOWNLOAD';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'GET',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'presignUrl': true
        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
        const data = await response.json();
        return data;
    } catch (error) {
        console.error("Error in apiFilesListDownloadSingle:", error);
        throw error;
    }
}


async function apiFilesListDownloadZip(fileIds, filename) {
    const apiUrl = './api/files/downloadzip';
    const reqPerm = 'PERM_DOWNLOAD';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'GET',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'ids': fileIds,
            'filename': 'base64:' + Base64.encode(filename),
            'presignUrl': true
        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
        const data = await response.json();
        return data;
    } catch (error) {
        console.error("Error in apiFilesListDownloadZip:", error);
        throw error;
    }
}


async function apiFilesModify(id, allowedDownloads, expiry, password, originalPw) {
    const apiUrl = './api/files/modify';
    const reqPerm = 'PERM_EDIT';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'PUT',
        headers: {
            'Content-Type': 'application/json',
            'id': id,
            'apikey': token,
            'allowedDownloads': allowedDownloads,
            'expiryTimestamp': expiry,
            'password': password,
            'originalPassword': originalPw
        },
    };
    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
        const data = await response.json();
        return data;
    } catch (error) {
        console.error("Error in apiFilesModify:", error);
        throw error;
    }
}



async function apiFilesDelete(id, delay) {
    const apiUrl = './api/files/delete';
    const reqPerm = 'PERM_DELETE';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'id': id,
            'delay': delay
        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
    } catch (error) {
        console.error("Error in apiFilesDelete:", error);
        throw error;
    }
}


async function apiFilesRestore(id) {
    const apiUrl = './api/files/restore';
    const reqPerm = 'PERM_DELETE';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'id': id
        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
        const data = await response.json();
        return data;
    } catch (error) {
        console.error("Error in apiFilesRestore:", error);
        throw error;
    }
}



// users


async function apiUserCreate(userName) {
    const apiUrl = './api/user/create';
    const reqPerm = 'PERM_MANAGE_USERS';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'username': encodeHeaderText(userName)

        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            if (response.status == 409) {
                throw new Error("duplicate");
            }
            throw await apiFailure(response);
        }
        const data = await response.json();
        return data;
    } catch (error) {
        console.error("Error in apiUserModify:", error);
        throw error;
    }
}


async function apiUserModify(userId, permission, modifier) {
    const apiUrl = './api/user/modify';
    const reqPerm = 'PERM_MANAGE_USERS';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'userid': userId,
            'userpermission': permission,
            'permissionModifier': modifier

        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
    } catch (error) {
        console.error("Error in apiUserModify:", error);
        throw error;
    }
}


async function apiUserChangeRank(userId, newRank) {
    const apiUrl = './api/user/changeRank';
    const reqPerm = 'PERM_MANAGE_USERS';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'userid': userId,
            'newRank': newRank

        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
    } catch (error) {
        console.error("Error in apiUserModify:", error);
        throw error;
    }
}

async function apiUserDelete(id, deleteFiles) {
    const apiUrl = './api/user/delete';
    const reqPerm = 'PERM_MANAGE_USERS';

    let token;
    let fileOperationKey;

    try {
        token = await getToken(reqPerm, false);
        fileOperationKey = await getToken(deleteFiles ? 'PERM_DELETE' : 'PERM_EDIT', false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'userid': id,
            'deleteFiles': deleteFiles,
            'transferToSelf': !deleteFiles,
            'fileOperationKey': fileOperationKey
        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
    } catch (error) {
        console.error("Error in apiUserDelete:", error);
        throw error;
    }
}



async function apiUserResetPassword(id, generatePw) {
    const apiUrl = './api/user/resetPassword';
    const reqPerm = 'PERM_MANAGE_USERS';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'userid': id,
            'generateNewPassword': generatePw
        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
        const data = await response.json();
        return data;
    } catch (error) {
        console.error("Error in apiUserResetPassword:", error);
        throw error;
    }
}


// Logs


async function apiLogSystemStatus() {
    const apiUrl = './api/logs/systemStatus';
    const reqPerm = 'PERM_MANAGE_LOGS';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'GET',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token
        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
        const data = await response.json();
        return data;
    } catch (error) {
        console.error("Error in apiLogSystemStatus:", error);
        throw error;
    }
}

async function apiLogResetTraffic() {
    const apiUrl = './api/logs/resetTraffic';
    const reqPerm = 'PERM_MANAGE_LOGS';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'GET',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token
        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
    } catch (error) {
        console.error("Error in apiLogResetTraffic:", error);
        throw error;
    }
}

async function apiLogGet(timestamp) {
    const apiUrl = './api/logs/get';
    const reqPerm = 'PERM_MANAGE_LOGS';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'GET',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'timestamp': timestamp
        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
        const data = await response.json();
        return data;
    } catch (error) {
        console.error("Error in apiLogGet:", error);
        throw error;
    }
}


async function apiLogsDelete(timestamp) {
    const apiUrl = './api/logs/delete';
    const reqPerm = 'PERM_MANAGE_LOGS';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'timestamp': timestamp
        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
    } catch (error) {
        console.error("Error in apiLogsDelete:", error);
        throw error;
    }
}

// E2E


async function apiE2eGet(lease) {
    if (!lease) throw new Error("Missing E2E lease; retry the operation");
    const apiUrl = './api/e2e/get';
    const reqPerm = 'PERM_UPLOAD';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'e2eLease': lease || ''
        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
        return await response.text();
    } catch (error) {
        console.error("Error in apiE2eGet:", error);
        throw error;
    }
}


async function apiE2eMutexLockUnlock(doUnlock, lease, signal) {
    if (doUnlock && !lease) throw new Error("Missing E2E lease");
    let apiUrl = './api/e2e/mutex/lock';
    if (doUnlock) {
    	apiUrl = './api/e2e/mutex/unlock';
    }
    const reqPerm = 'PERM_UPLOAD';

    let token;

    try {
        token = await e2eAbortable(getToken(reqPerm, false), signal);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'GET',
        signal,
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'e2eLease': lease || ''
        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            const error = await apiFailure(response);
            if (response.status === 409 || response.status === 429) {
                error.e2eAdmissionBusy = true;
                error.message = "E2E metadata is busy; retry the operation. " + error.message;
            }
            throw error;
        }
        if (doUnlock) return;
        const result = await response.json();
        if (!result.leaseToken) throw new Error("Server did not provide an E2E lease; reload after upgrading");
        return result.leaseToken;
    } catch (error) {
        console.error("Error in apiE2eMutexLock:", error);
        throw error;
    }
}



async function apiE2eStore(content, lease) {
    if (!lease) throw new Error("Missing E2E lease; retry the operation");
    const apiUrl = './api/e2e/set';
    const reqPerm = 'PERM_UPLOAD';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'e2eLease': lease || ''
        },
        body: JSON.stringify({
            'content': content
        }),
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
    } catch (error) {
        console.error("Error in apiE2eStore:", error);
        throw error;
    }
}


// Keep the lease local to this read/modify/write transaction. Never release
// after a failed acquisition, and never replay a non-idempotent callback.
function e2eAbortable(promise, signal) {
    if (!signal) return promise;
    if (signal.aborted) {
        Promise.resolve(promise).catch(() => {});
        return Promise.reject(signal.reason || new Error("E2E acquisition cancelled"));
    }
    return new Promise((resolve, reject) => {
        const abort = () => reject(signal.reason || new Error("E2E acquisition cancelled"));
        signal.addEventListener('abort', abort, {once:true});
        promise.then(resolve, reject).finally(() => signal.removeEventListener('abort', abort));
    });
}

async function acquireE2eLease({signal} = {}) {
    const controller = new AbortController();
    const abort = () => controller.abort(signal.reason);
    if (signal) {
        if (signal.aborted) abort();
        else signal.addEventListener('abort', abort, {once:true});
    }
    const timeout = setTimeout(() => controller.abort(new Error("E2E acquisition timed out; retry the operation")), 2000);
    try {
        for (let attempt = 0; attempt < 4; attempt++) {
            try {
                return await apiE2eMutexLockUnlock(false, undefined, controller.signal);
            } catch (error) {
                if (controller.signal.aborted || !error.e2eAdmissionBusy || attempt === 3) throw error;
                await new Promise((resolve, reject) => {
                    const abortDelay = () => {
                        clearTimeout(timer);
                        reject(controller.signal.reason || new Error("E2E acquisition cancelled"));
                    };
                    const timer = setTimeout(() => {
                        controller.signal.removeEventListener('abort', abortDelay);
                        resolve();
                    }, (attempt + 1) * 100);
                    controller.signal.addEventListener('abort', abortDelay, {once:true});
                    if (controller.signal.aborted) abortDelay();
                });
            }
        }
    } finally {
        clearTimeout(timeout);
        if (signal) signal.removeEventListener('abort', abort);
    }
}

async function withE2eLease(operation, {signal} = {}) {
    const lease = await acquireE2eLease({signal});
    try {
        if (signal && signal.aborted) throw signal.reason || new Error("E2E acquisition cancelled");
        return await operation(lease);
    } finally {
        try { await apiE2eMutexLockUnlock(true, lease); }
        catch (error) { console.error("Failed to release E2E lease; it will expire", error); }
    }
}

// Upload Requests

async function apiQuotaSummary() {
    try {
        const token = await getToken('PERM_VIEW', false);
        const response = await fetch('./api/quota/summary', {method: 'GET', headers: {apikey: token}, cache: 'no-store'});
        if (!response.ok) throw await apiFailure(response);
        return await response.json();
    } catch (failure) {
        if (failure.status === 401 || failure.status === 403) storedTokens.delete('PERM_VIEW');
        throw failure;
    }
}

async function apiURequestList(visibleIds) {
    try {
        const token = await getToken('PERM_MANAGE_FILE_REQUESTS', false);
        // A paginated list is not a complete authorization snapshot. Refresh
        // only rendered IDs, with bounded parallelism, including on older pages.
        if (Array.isArray(visibleIds)) {
            const ids = [...new Set(visibleIds)];
            const requests = [];
            let next = 0, serverTime = null;
            await Promise.all(Array.from({length: Math.min(4, ids.length)}, async () => {
                while (next < ids.length) {
                    const id = ids[next++];
                    const response = await fetch('./api/uploadrequest/list/' + encodeURIComponent(id), {
                        method: 'GET', headers: {apikey: token}, cache: 'no-store'
                    });
                    if (response.status === 404) continue;
                    if (!response.ok) throw await apiFailure(response);
                    const record = await response.json();
                    if (!record || record.id !== id) throw new Error('invalid collection summary');
                    requests.push(record);
                    const date = Date.parse(response.headers.get('Date'));
                    if (Number.isFinite(date)) serverTime = date / 1000;
                }
            }));
            return {requests, serverTime};
        }
        const response = await fetch('./api/uploadrequest/list', {
            method: 'GET', headers: {apikey: token}, cache: 'no-store'
        });
        if (!response.ok) throw await apiFailure(response);
        const requests = await response.json();
        const serverDate = Date.parse(response.headers.get('Date'));
        return {requests, serverTime: Number.isFinite(serverDate) ? serverDate / 1000 : null, nextCursor: response.headers.get("X-Next-Cursor") || ""};
    } catch (failure) {
        if (failure.status === 401 || failure.status === 403) storedTokens.delete('PERM_MANAGE_FILE_REQUESTS');
        throw failure;
    }
}

async function apiURequestDelete(id) {
    const apiUrl = './api/uploadrequest/delete';
    const reqPerm = 'PERM_MANAGE_FILE_REQUESTS';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'DELETE',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'id': id
        },
    };

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
    } catch (error) {
        console.error("Error in apiURequestDelete:", error);
        throw error;
    }
}



async function apiURequestSave(id, name, maxfiles, maxsize, expiry, notes, allowedDownloads) {
    const apiUrl = './api/uploadrequest/save';
    const reqPerm = 'PERM_MANAGE_FILE_REQUESTS';

    let token;

    try {
        token = await getToken(reqPerm, false);
    } catch (error) {
        console.error("Unable to gain permission token:", error);
        throw error;
    }

    const requestOptions = {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'apikey': token,
            'id': id,
            'name': 'base64:' + Base64.encode(name),
            'expiry': expiry,
            'maxfiles': maxfiles,
            'maxsize': maxsize,
            'notes': 'base64:' + Base64.encode(notes),
        },
    };

    if (allowedDownloads !== undefined) requestOptions.headers.allowedDownloads = allowedDownloads;

    try {
        const response = await fetch(apiUrl, requestOptions);
        if (!response.ok) {
            throw await apiFailure(response);
        }
        const data = await response.json();
        return data;
    } catch (error) {
        console.error("Error in apiURequestDelete:", error);
        throw error;
    }
}
