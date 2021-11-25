setTimeout(function() {
  const callbackName = 'callback_' + new Date().getTime();
  window[callbackName] = function (response) {
  const div = document.createElement('div');
  div.innerHTML = response.html;
  document.querySelector(".md-header__inner > .md-header__title").appendChild(div);
  const container = div.querySelector('.rst-versions');
  var caret = document.createElement('div');
  caret.innerHTML = "<i class='fa fa-caret-down dropdown-caret'></i>"
  caret.classList.add('dropdown-caret')
  div.querySelector('.rst-current-version').appendChild(caret);
  div.querySelector('.rst-current-version').addEventListener('click', function() {
      const classes = container.className.split(' ');
      const index = classes.indexOf('shift-up');
      if (index === -1) {
          classes.push('shift-up');
      } else {
          classes.splice(index, 1);
      }
      container.className = classes.join(' ');
  });
  }

  var CSSLink = document.createElement('link');
  CSSLink.rel='stylesheet';
  CSSLink.href = '/assets/versions.css';
  document.getElementsByTagName('head')[0].appendChild(CSSLink);

  var script = document.createElement('script');
  script.src = 'https://argo-rollouts.readthedocs.io/_/api/v2/footer_html/?'+
      'callback=' + callbackName + '&project=argo-rollouts&page=&theme=mkdocs&format=jsonp&docroot=docs&source_suffix=.md&version=' + (window['READTHEDOCS_DATA'] || { version: 'latest' }).version;
  document.getElementsByTagName('head')[0].appendChild(script);
}, 0);


