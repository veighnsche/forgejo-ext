import $ from 'jquery';
import {htmlEscape} from 'escape-goat';
import {hideElem, showElem} from '../utils/dom.js';
import {contributionRootSelector} from '../native-lifecycle.js';

const {appSubUrl} = window.config;
let templateRoot = null;
let templateSearch = null;
let templateOwner = null;

export function retireRepoTemplateSearch() {
  if (templateRoot) $(templateRoot).off('.repoTemplate');
  if (templateOwner) $(templateOwner).off('.repoTemplate');
  if (templateSearch) $(templateSearch).dropdown('destroy');
  templateRoot = null;
  templateSearch = null;
  templateOwner = null;
}

export function initRepoTemplateSearch() {
  retireRepoTemplateSearch();
  const $repoTemplate = $('#repo_template');
  if (!$repoTemplate.length || $repoTemplate[0].closest(contributionRootSelector)) return;
  templateRoot = $repoTemplate[0];
  templateSearch = document.getElementById('repo_template_search');
  templateOwner = document.getElementById('uid');
  const checkTemplate = function () {
    const $templateUnits = $('#template_units');
    const $nonTemplate = $('#non_template');
    if ($repoTemplate.val() !== '' && $repoTemplate.val() !== '0') {
      showElem($templateUnits);
      hideElem($nonTemplate);
    } else {
      hideElem($templateUnits);
      showElem($nonTemplate);
    }
  };
  $repoTemplate.on('change.repoTemplate', checkTemplate);
  checkTemplate();

  const changeOwner = function () {
    $('#repo_template_search')
      .dropdown({
        apiSettings: {
          url: `${appSubUrl}/repo/search?q={query}&template=true&priority_owner_id=${$('#uid').val()}`,
          onResponse(response) {
            const filteredResponse = {success: true, results: []};
            filteredResponse.results.push({
              name: '',
              value: '',
            });
            // Parse the response from the api to work with our dropdown
            for (const repo of response.data) {
              filteredResponse.results.push({
                name: htmlEscape(repo.repository.full_name),
                value: repo.repository.id,
              });
            }
            return filteredResponse;
          },
          cache: false,
        },

        fullTextSearch: true,
      });
  };
  $('#uid').on('change.repoTemplate', changeOwner);
  changeOwner();
}
