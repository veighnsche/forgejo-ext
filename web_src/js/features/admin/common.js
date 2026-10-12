import $ from 'jquery';
import {checkAppUrl} from '../common-global.js';
import {hideElem, showElem, toggleElem} from '../../utils/dom.js';
import {POST} from '../../modules/fetch.js';
import {showModal} from '../../modules/modal.ts';

const {appSubUrl} = window.config;

// Attachments made by init as [element, type, handler, isJQuery]; retire
// removes exactly what init added.
const attached = [];
let initialized = false;

function attachNative(el, type, handler) {
  if (!el) return;
  el.addEventListener(type, handler);
  attached.push([el, type, handler, false]);
}

function attachJQuery(el, type, handler) {
  $(el).on(type, handler);
  attached.push([el, type, handler, true]);
}

function onSecurityProtocolChange() {
  if (Number(document.getElementById('security_protocol')?.value) > 0) {
    showElem('.has-tls');
  } else {
    hideElem('.has-tls');
  }
}

function onLoginTypeChange(event) {
  const el = event.currentTarget;
  if (el.value?.substring(0, 1) === '0') {
    document.getElementById('user_name')?.removeAttribute('disabled');
    document.getElementById('login_name')?.removeAttribute('required');
    hideElem('.non-local');
    showElem('.local');
    document.getElementById('user_name')?.focus();

    if (el.getAttribute('data-password') === 'required') {
      document.getElementById('password')?.setAttribute('required', 'required');
    }
  } else {
    if (document.querySelector('.admin.edit.user')) {
      document.getElementById('user_name')?.setAttribute('disabled', 'disabled');
    }
    document.getElementById('login_name')?.setAttribute('required', 'required');
    showElem('.non-local');
    hideElem('.local');
    document.getElementById('login_name')?.focus();

    document.getElementById('password')?.removeAttribute('required');
  }
}

function onUsePagedSearchChange() {
  const searchPageSizeElements = document.querySelectorAll('.search-page-size');
  if (document.getElementById('use_paged_search').checked) {
    showElem('.search-page-size');
    for (const el of searchPageSizeElements) {
      el.querySelector('input')?.setAttribute('required', 'required');
    }
  } else {
    hideElem('.search-page-size');
    for (const el of searchPageSizeElements) {
      el.querySelector('input')?.removeAttribute('required');
    }
  }
}

function onOAuth2UseCustomURLChange(applyDefaultValues) {
  const provider = document.getElementById('oauth2_provider')?.value;
  hideElem('.oauth2_use_custom_url_field');
  for (const input of document.querySelectorAll('.oauth2_use_custom_url_field input[required]')) {
    input.removeAttribute('required');
  }

  if (document.getElementById('oauth2_use_custom_url')?.checked) {
    for (const custom of ['token_url', 'auth_url', 'profile_url', 'email_url', 'tenant']) {
      const customInput = document.getElementById(`${provider}_${custom}`);
      if (!customInput) continue;
      if (applyDefaultValues) {
        document.getElementById(`oauth2_${custom}`).value = customInput.value;
      }
      if (customInput.getAttribute('data-available') === 'true') {
        for (const input of document.querySelectorAll(`.oauth2_${custom} input`)) {
          input.setAttribute('required', 'required');
        }
        showElem(`.oauth2_${custom}`);
      }
    }
  }
}

function onOAuth2Change(applyDefaultValues) {
  hideElem('.open_id_connect_auto_discovery_url, .oauth2_use_custom_url, .oauth2_attribute_ssh_public_key');
  for (const input of document.querySelectorAll('.open_id_connect_auto_discovery_url input[required]')) {
    input.removeAttribute('required');
  }

  const provider = document.getElementById('oauth2_provider')?.value;
  switch (provider) {
    case 'openidConnect':
      for (const input of document.querySelectorAll('.open_id_connect_auto_discovery_url input')) {
        input.setAttribute('required', 'required');
      }
      showElem('.open_id_connect_auto_discovery_url');
      break;
    default: {
      const customURLSettings = document.getElementById(`${provider}_customURLSettings`);
      if (!customURLSettings) break;
      const customURLRequired = (customURLSettings.getAttribute('data-required') === 'true');
      document.getElementById('oauth2_use_custom_url').checked = customURLRequired;
      if (customURLRequired || customURLSettings.getAttribute('data-available') === 'true') {
        showElem('.oauth2_use_custom_url');
      }
    }
  }
  const canProvideSSHKeys = document.getElementById(`${provider}_canProvideSSHKeys`);
  if (canProvideSSHKeys) {
    showElem('.oauth2_attribute_ssh_public_key');
  }
  onOAuth2UseCustomURLChange(applyDefaultValues);
}

const onOAuth2ProviderChange = () => onOAuth2Change(true);
const onOAuth2CustomURLChangeApply = () => onOAuth2UseCustomURLChange(true);
const onOAuth2CustomURLChangeKeep = () => onOAuth2UseCustomURLChange(false);

function onEnableLdapGroupsChange() {
  toggleElem(document.getElementById('ldap-group-options'), $('.js-ldap-group-toggle')[0].checked);
}

function onAuthTypeChange(event) {
  hideElem('.ldap, .dldap, .smtp, .pam, .oauth2, .has-tls, .search-page-size');

  for (const input of document.querySelectorAll('.ldap input[required], .binddnrequired input[required], .dldap input[required], .smtp input[required], .pam input[required], .oauth2 input[required], .has-tls input[required]')) {
    input.removeAttribute('required');
  }

  document.querySelector('.binddnrequired')?.classList.remove('required');

  const authType = event.currentTarget.value;
  switch (authType) {
    case '2': // LDAP
      showElem('.ldap');
      for (const input of document.querySelectorAll('.binddnrequired input, .ldap div.required:not(.dldap) input')) {
        input.setAttribute('required', 'required');
      }
      document.querySelector('.binddnrequired')?.classList.add('required');
      break;
    case '3': // SMTP
      showElem('.smtp');
      showElem('.has-tls');
      for (const input of document.querySelectorAll('.smtp div.required input, .has-tls')) {
        input.setAttribute('required', 'required');
      }
      break;
    case '4': // PAM
      showElem('.pam');
      for (const input of document.querySelectorAll('.pam input')) {
        input.setAttribute('required', 'required');
      }
      break;
    case '5': // LDAP
      showElem('.dldap');
      for (const input of document.querySelectorAll('.dldap div.required:not(.ldap) input')) {
        input.setAttribute('required', 'required');
      }
      break;
    case '6': // OAuth2
      showElem('.oauth2');
      for (const input of document.querySelectorAll('.oauth2 div.required:not(.oauth2_use_custom_url,.oauth2_use_custom_url_field,.open_id_connect_auto_discovery_url) input')) {
        input.setAttribute('required', 'required');
      }
      onOAuth2Change(true);
      break;
  }
  if (authType === '2' || authType === '5') {
    onSecurityProtocolChange();
    onEnableLdapGroupsChange();
  }
  if (authType === '2') {
    onUsePagedSearchChange();
  }
}

function onAuthNameInput(event) {
  // appSubUrl is either empty or is a path that starts with `/` and doesn't have a trailing slash.
  document.getElementById('oauth2-callback-url').textContent = `${window.location.origin}${appSubUrl}/user/oauth2/${encodeURIComponent(event.target.value)}/callback`;
}

function onViewDetailClick(event) {
  const detailModal = document.getElementById('detail-modal');
  const description = event.currentTarget.closest('tr').querySelector('.notice-description').textContent;
  detailModal.querySelector('.content pre').textContent = description;
  showModal('detail-modal', undefined);
  return false;
}

function onSelectActionClick(event) {
  const checkboxes = document.querySelectorAll('.select.table .ui.checkbox input');
  switch ($(event.currentTarget).data('action')) {
    case 'select-all':
      for (const checkbox of checkboxes) {
        checkbox.checked = true;
      }
      break;
    case 'deselect-all':
      for (const checkbox of checkboxes) {
        checkbox.checked = false;
      }
      break;
    case 'inverse':
      for (const checkbox of checkboxes) {
        checkbox.checked = !checkbox.checked;
      }
      break;
  }
}

async function onDeleteSelectionClick(e) {
  e.preventDefault();
  const el = e.currentTarget;
  el.classList.add('is-loading', 'disabled');
  const data = new FormData();
  for (const checkbox of document.querySelectorAll('.select.table .ui.checkbox input')) {
    if (checkbox.checked) {
      data.append('ids[]', checkbox.closest('.ui.checkbox').getAttribute('data-id'));
    }
  }
  await POST(el.getAttribute('data-link'), {data});
  window.location.href = el.getAttribute('data-redirect');
}

export function initAdminCommon() {
  if (!document.querySelector('.page-content.admin')) return;
  if (initialized) return;
  initialized = true;

  // check whether appUrl(ROOT_URL) is correct, if not, show an error message
  checkAppUrl();

  // New user
  if ($('.admin.new.user').length > 0 || $('.admin.edit.user').length > 0) {
    attachNative(document.getElementById('login_type'), 'change', onLoginTypeChange);
  }

  // New authentication
  if (document.querySelector('.admin.new.authentication')) {
    attachNative(document.getElementById('auth_type'), 'change', onAuthTypeChange);
    document.getElementById('auth_type').dispatchEvent(new Event('change'));
    attachNative(document.getElementById('security_protocol'), 'change', onSecurityProtocolChange);
    attachNative(document.getElementById('use_paged_search'), 'change', onUsePagedSearchChange);
    attachNative(document.getElementById('oauth2_provider'), 'change', onOAuth2ProviderChange);
    attachNative(document.getElementById('oauth2_use_custom_url'), 'change', onOAuth2CustomURLChangeApply);
    for (const el of document.querySelectorAll('.js-ldap-group-toggle')) {
      attachJQuery(el, 'change', onEnableLdapGroupsChange);
    }
  }
  // Edit authentication
  if (document.querySelector('.admin.edit.authentication')) {
    const authType = document.getElementById('auth_type')?.value;
    if (authType === '2' || authType === '5') {
      attachNative(document.getElementById('security_protocol'), 'change', onSecurityProtocolChange);
      for (const el of document.querySelectorAll('.js-ldap-group-toggle')) {
        attachJQuery(el, 'change', onEnableLdapGroupsChange);
      }
      onEnableLdapGroupsChange();
      if (authType === '2') {
        attachNative(document.getElementById('use_paged_search'), 'change', onUsePagedSearchChange);
      }
    } else if (authType === '6') {
      attachNative(document.getElementById('oauth2_provider'), 'change', onOAuth2ProviderChange);
      attachNative(document.getElementById('oauth2_use_custom_url'), 'change', onOAuth2CustomURLChangeKeep);
      onOAuth2Change(false);
    }
  }

  if (document.querySelector('.admin.edit.authentication, .admin.new.authentication')) {
    const authNameEl = document.getElementById('auth_name');
    attachNative(authNameEl, 'input', onAuthNameInput);
    authNameEl.dispatchEvent(new Event('input'));
  }

  // Notice
  if (document.querySelector('.admin.notice')) {
    // Attach view detail modals
    for (const el of document.querySelectorAll('.view-detail')) {
      attachJQuery(el, 'click', onViewDetailClick);
    }

    // Select actions
    for (const el of document.querySelectorAll('.select.action')) {
      attachJQuery(el, 'click', onSelectActionClick);
    }
    attachNative(document.getElementById('delete-selection'), 'click', onDeleteSelectionClick);
  }
}

export function retireAdminCommon() {
  for (const [el, type, handler, isJQuery] of attached) {
    if (isJQuery) {
      $(el).off(type, handler);
    } else {
      el.removeEventListener(type, handler);
    }
  }
  attached.length = 0;
  initialized = false;
}
