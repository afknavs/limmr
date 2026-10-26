/* Local, read-only print measurement; no network, storage or external service. */
(async () => { try {
  // Headless virtual time can leave decode() pending; layout only needs the images loaded.
  const settle = ms => new Promise(resolve => setTimeout(resolve, ms));
  await Promise.race([document.fonts.ready, settle(5000)]);
  await Promise.race([Promise.all(Array.from(document.images, image => image.complete ? null :
    new Promise(resolve => { image.onload = image.onerror = resolve; }))), settle(15000)]);
  // Fixed 14 pt text gaps; allocate only the remaining height to the native image.
  // Never enlarge past --print-width, whose worst-case ppi is checked before Chrome.
  for (const page of document.querySelectorAll('.journey')) {
    const body = page.querySelector('.body'), figure = page.querySelector('.journey-screen');
    const image = figure.querySelector('img');
    const children = [...body.children], gap = parseFloat(getComputedStyle(body).rowGap);
    const imageRect = image.getBoundingClientRect();
    const nonImage = figure.getBoundingClientRect().height - imageRect.height +
      children.filter(node => node !== figure).reduce((sum, node) => sum + node.getBoundingClientRect().height, 0) +
      gap * (children.length - 1);
    const maxHeight = Math.max(0, body.getBoundingClientRect().height - nonImage);
    const width = Math.min(imageRect.width, maxHeight * Number(image.width) / Number(image.height));
    figure.style.setProperty('--render-width', `${width}px`);
  }
  const results = Array.from(document.querySelectorAll('.page'), (page, index) => {
    const footer = page.querySelector('footer').getBoundingClientRect();
    const body = page.querySelector('.body'), nodes = [...body.querySelectorAll('*')];
    const last = Math.max(...nodes.map(node => node.getBoundingClientRect().bottom));
    const headings = [...page.querySelectorAll('h1,h2,h3')].map(node => ({
      text: node.textContent, tracking: getComputedStyle(node).letterSpacing,
      wordSpacing: getComputedStyle(node).wordSpacing
    }));
    const images = [...page.querySelectorAll('img')].map(image => {
      const rect = image.getBoundingClientRect();
      return {src:image.getAttribute('src'),
        xPpi:Number(image.getAttribute('width')) * 96 / rect.width,
        yPpi:Number(image.getAttribute('height')) * 96 / rect.height,
        uiTextPt: Number(image.dataset.uiReferencePt) * rect.width * 72 / 96 / Number(image.dataset.logicalWidth)};
    });
    const orphanHeadings = [...page.querySelectorAll('h1,h2,h3')].filter(node => {
      const next = node.nextElementSibling;
      return !next || next.getBoundingClientRect().top > footer.top ||
        node.getBoundingClientRect().bottom > footer.top;
    }).map(node => node.textContent);
    const overflow = nodes.some(node => {
      const rect = node.getBoundingClientRect(), bounds = page.getBoundingClientRect();
      return rect.bottom > footer.top - 8 || rect.right > bounds.right || rect.left < bounds.left;
    });
    const textBlocks = [...page.querySelectorAll('figcaption,.journey-context,.underneath,.source')];
    const textGapsPt = textBlocks.slice(1).map((node, i) =>
      (node.getBoundingClientRect().top - textBlocks[i].getBoundingClientRect().bottom) * 72 / 96);
    page.dataset.layoutOverflow = String(overflow);
    return {page:index+1, heading:page.querySelector('h1,h2').textContent,
      journey:page.classList.contains('journey'), overflow,
      gapToFooterMm:Math.round((footer.top-last)*25.4/96),
      blankEndRatio:Math.max(0,footer.top-last)/page.getBoundingClientRect().height,
      textGapsPt, headings, images, orphanHeadings,
      brokenImages:[...page.querySelectorAll('img')].filter(i=>!i.naturalWidth).length};
  });
  document.body.dataset.layoutCheck = results.every(p => !p.overflow && p.brokenImages===0) ? 'pass' : 'fail';
  const report = document.createElement('script'); report.id='layout-report';
  report.type='application/json'; report.textContent=JSON.stringify(results);
  document.body.append(report);
} catch (error) {
  const report = document.createElement('script'); report.id='layout-report';
  report.type='application/json'; report.textContent=JSON.stringify({error: String(error && error.stack || error)});
  document.body.append(report);
} })();
