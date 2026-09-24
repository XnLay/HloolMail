import {
  ArrowRight,
  Bot,
  Check,
  CircleUserRound,
  Code2,
  Copy as CopyIcon,
  Github,
  Globe2,
  Inbox,
  LockKeyhole,
  MailCheck,
  MailPlus,
  Network,
  PackageCheck,
  Share2,
  Sparkles,
  Terminal,
  Users,
  Zap,
  type LucideIcon,
} from 'lucide-react';
import { useMemo } from 'react';
import type { InstallStatus } from '../api';
import { HeaderSettings } from '../components/layout/HeaderSettings';
import { InfoTip } from '../components/shared';
import { AppLogo } from '../components/shared/AppLogo';
import { usePublicLoginSettings } from '../features/auth/queries';
import { useLandingMotion } from '../features/landing/useLandingMotion';
import { useCopyState } from '../hooks/useCopyState';
import { useCountUp } from '../hooks/useCountUp';
import { copy } from '../lib/clipboard';
import { useText } from '../locales';
import '../styles/auth.css';
import '../styles/landing.css';
import '../styles/layout.css';
import '../styles/settings.css';

type LandingPageProps = { status?: InstallStatus; statsLoading?: boolean };

export function LandingPage({ status, statsLoading = false }: LandingPageProps) {
  const { pageRef, headerRef, activeLandingSection, scrollToLandingSection } = useLandingMotion();
  const text = useText();

  const mxTarget = (
    status?.config?.expected_mx ||
    status?.config?.mail_hostname ||
    'mail.example.com'
  ).replace(/\.$/, '');

  const siteApiCallsToday = status?.site_api_calls_today ?? 0;

  const registeredUsers = status?.registered_users ?? 0;

  const hostedDomains = status?.hosted_domains ?? 0;

  const publicStatsLoading = statsLoading;

  const proofLine = text.login.proofLine
    .replace('{users}', registeredUsers.toLocaleString())
    .replace('{domains}', hostedDomains.toLocaleString());

  const [mxCopied, markMxCopied] = useCopyState();

  const { registrationAvailable } = usePublicLoginSettings();
  return (
    <div ref={pageRef} className={'landing-page'}>
      <a href={'#home-main'} className="skip-to-content">
        {text.login.skipToContent ?? '跳到主要内容'}
      </a>
      <header ref={headerRef} className="landing-header">
        <div className="landing-brand">
          <span className="app-header-brand-mark">
            <AppLogo />
          </span>
          <span>HLOOL Mail</span>
        </div>
        <nav className="landing-nav">
          {
            <div className="landing-nav-sections" aria-label={text.login.featuresSectionTitle}>
              <a
                href="#how"
                className={activeLandingSection === 'how' ? 'landing-nav-active' : undefined}
                aria-current={activeLandingSection === 'how' ? 'page' : undefined}
                onClick={(event) => scrollToLandingSection(event, 'how')}
              >
                {text.login.howTitle}
              </a>
              <a
                href="#features"
                className={activeLandingSection === 'features' ? 'landing-nav-active' : undefined}
                aria-current={activeLandingSection === 'features' ? 'page' : undefined}
                onClick={(event) => scrollToLandingSection(event, 'features')}
              >
                {text.login.featuresSectionTitle}
              </a>
              <a
                href="#use-cases"
                className={activeLandingSection === 'use-cases' ? 'landing-nav-active' : undefined}
                aria-current={activeLandingSection === 'use-cases' ? 'page' : undefined}
                onClick={(event) => scrollToLandingSection(event, 'use-cases')}
              >
                {text.login.useCasesTitle}
              </a>
              <a
                href="#deploy"
                className={activeLandingSection === 'deploy' ? 'landing-nav-active' : undefined}
                aria-current={activeLandingSection === 'deploy' ? 'page' : undefined}
                onClick={(event) => scrollToLandingSection(event, 'deploy')}
              >
                {text.login.deployNav}
              </a>
            </div>
          }
          {
            <a
              className="landing-nav-link landing-nav-icon"
              href="#/login"
              aria-label={text.login.loginTab}
            >
              <CircleUserRound size={18} />
            </a>
          }
          <a
            className="landing-nav-link landing-nav-icon"
            href="https://github.com/hloolx/HloolMail"
            target="_blank"
            rel="noopener noreferrer"
            aria-label="GitHub"
          >
            <Github size={17} />
          </a>
        </nav>
        <HeaderSettings />
      </header>

      <main id={'home-main'} className={'landing-main landing-main-public'}>
        {
          <section className="landing-hero">
            <div className="landing-hero-top">
              <div className="landing-hero-copy">
                <div className="landing-kicker">
                  <Sparkles size={14} />
                  {text.login.homeBadge}
                </div>
                <h1>HLOOL Mail</h1>
                <p className="landing-hero-lead">{text.login.homeTitle}</p>
                <div className="landing-actions">
                  {registrationAvailable && (
                    <button
                      className="btn-primary"
                      type="button"
                      onClick={() => {
                        window.location.hash = '#/register';
                      }}
                    >
                      <MailPlus size={16} />
                      {text.login.primaryAction}
                    </button>
                  )}
                  <a
                    className="btn-secondary"
                    href="https://github.com/hloolx/HloolMail"
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    <Github size={16} />
                    {text.login.secondaryAction}
                  </a>
                </div>
                <div className="landing-hero-proof">
                  <span>{proofLine}</span>
                </div>
              </div>

              <aside className="landing-dns-config-card" aria-label={text.login.mxConfigTitle}>
                <div className="landing-dns-config-head">
                  <span>
                    <Network size={16} />
                    {text.login.mxConfigTitle}
                  </span>
                  <div className="landing-mx-address">
                    <div>
                      <span>{text.login.mxConfigValue}</span>
                      <code>{mxTarget}</code>
                    </div>
                    <button
                      type="button"
                      className="landing-mx-copy-button"
                      aria-label={mxCopied ? text.common.copied : text.common.copy}
                      title={mxCopied ? text.common.copied : text.common.copy}
                      onClick={(event) => {
                        void copy(mxTarget, {
                          event,
                          celebrate: true,
                          label: text.common.copied,
                        }).then((ok) => {
                          if (ok) markMxCopied();
                        });
                      }}
                    >
                      {mxCopied ? <Check size={15} /> : <CopyIcon size={15} />}
                    </button>
                  </div>
                </div>
                <div className="landing-dns-config-list">
                  <div className="landing-dns-config-row">
                    <span>{text.login.mxConfigType}</span>
                    <b>MX</b>
                  </div>
                  <div className="landing-dns-config-row">
                    <span className="landing-dns-label-with-tip">
                      {text.login.mxConfigHost}
                      <InfoTip text={text.login.mxConfigHostInfo} />
                    </span>
                    <b>{text.login.mxConfigHostValue}</b>
                  </div>
                  <div className="landing-dns-config-row">
                    <span>{text.login.mxConfigPriority}</span>
                    <b>10</b>
                  </div>
                </div>
                <p>{text.login.domainCardDesc}</p>
              </aside>
            </div>
            <section className="landing-stats" aria-label={proofLine}>
              <div className="landing-stats-grid">
                <LandingStat
                  icon={Users}
                  value={registeredUsers}
                  label={text.login.statUsers}
                  loading={publicStatsLoading}
                />
                <LandingStat
                  icon={Globe2}
                  value={hostedDomains}
                  label={text.login.statHostedDomains}
                  loading={publicStatsLoading}
                />
                <LandingStat
                  icon={Zap}
                  value={siteApiCallsToday}
                  label={text.login.statApiToday}
                  loading={publicStatsLoading}
                />
              </div>
            </section>
          </section>
        }
      </main>

      {
        <>
          <section className="landing-how" id="how">
            <div className="landing-section-head" data-landing-reveal>
              <h2>{text.login.howTitle}</h2>
              <p>{text.login.howDesc}</p>
            </div>
            <div className="landing-how-steps landing-reveal-stagger">
              <div className="landing-how-step" data-landing-reveal>
                <span className="landing-how-num">01</span>
                <Network size={20} />
                <b>{text.login.flowDns}</b>
                <span>{text.login.flowDnsDesc.replace('{mx}', mxTarget)}</span>
              </div>
              <div className="landing-how-step" data-landing-reveal>
                <span className="landing-how-num">02</span>
                <Inbox size={20} />
                <b>{text.login.flowMailbox}</b>
                <span>{text.login.flowMailboxDesc}</span>
              </div>
              <div className="landing-how-step" data-landing-reveal>
                <span className="landing-how-num">03</span>
                <Code2 size={20} />
                <b>{text.login.flowApi}</b>
                <span>{text.login.flowApiDesc}</span>
              </div>
            </div>
          </section>

          <section className="landing-features" id="features">
            <div className="landing-section-head" data-landing-reveal>
              <h2>{text.login.featuresSectionTitle}</h2>
              <p>{text.login.featuresSectionDesc}</p>
            </div>
            <div className="landing-features-grid landing-reveal-stagger">
              <article data-landing-reveal>
                <Zap size={20} />
                <h3>{text.login.featureOneTitle}</h3>
                <p>{text.login.featureOneDesc}</p>
              </article>
              <article data-landing-reveal>
                <Code2 size={20} />
                <h3>{text.login.featureTwoTitle}</h3>
                <p>{text.login.featureTwoDesc}</p>
              </article>
              <article data-landing-reveal>
                <Share2 size={20} />
                <h3>{text.login.featureThreeTitle}</h3>
                <p>{text.login.featureThreeDesc}</p>
              </article>
              <article data-landing-reveal>
                <Terminal size={20} />
                <h3>{text.login.featureFourTitle}</h3>
                <p>{text.login.featureFourDesc}</p>
              </article>
              <article data-landing-reveal>
                <MailCheck size={20} />
                <h3>{text.login.featureFiveTitle}</h3>
                <p>{text.login.featureFiveDesc}</p>
              </article>
            </div>
          </section>

          <section className="landing-use-cases" id="use-cases">
            <div className="landing-section-head" data-landing-reveal>
              <h2>{text.login.useCasesTitle}</h2>
              <p>{text.login.useCasesDesc}</p>
            </div>
            <div className="landing-use-cases-grid landing-reveal-stagger">
              <article data-landing-reveal>
                <span className="landing-use-case-icon">
                  <Bot size={20} />
                </span>
                <b>{text.login.useCaseBatchTitle}</b>
                <p>{text.login.useCaseBatchDesc}</p>
              </article>
              <article data-landing-reveal>
                <span className="landing-use-case-icon">
                  <PackageCheck size={20} />
                </span>
                <b>{text.login.useCaseAccountTitle}</b>
                <p>{text.login.useCaseAccountDesc}</p>
              </article>
              <article data-landing-reveal>
                <span className="landing-use-case-icon">
                  <LockKeyhole size={20} />
                </span>
                <b>{text.login.useCasePrivacyTitle}</b>
                <p>{text.login.useCasePrivacyDesc}</p>
              </article>
            </div>
          </section>

          <section className="landing-overview">
            <div className="landing-overview-copy landing-reveal-copy" data-landing-reveal>
              <span>{text.login.overviewEyebrow}</span>
              <h2>{text.login.overviewTitle}</h2>
              <p>{text.login.overviewDesc}</p>
            </div>
            <div
              className="landing-overview-api landing-reveal-panel"
              data-landing-reveal
              aria-hidden
            >
              <div className="landing-api-window-bar">
                <i />
                <i />
                <i />
                <b>hlool-mail - bash</b>
              </div>
              <div className="landing-api-window-body">
                <div className="landing-api-line">
                  <b>POST</b>
                  <code>/api/generate-email</code>
                  <span>{text.login.apiLineCreate}</span>
                </div>
                <div className="landing-api-line">
                  <b>GET</b>
                  <code>/api/emails</code>
                  <span>{text.login.apiLineReceive}</span>
                </div>
                <div className="landing-api-line">
                  <b>GET</b>
                  <code>/api/emails/:id</code>
                  <span>{text.login.apiLineDetail}</span>
                </div>
                <div className="landing-api-prompt">
                  <span>$</span>
                  <span className="landing-api-prompt-cursor" />
                </div>
              </div>
            </div>
          </section>

          <section className="landing-deploy" id="deploy">
            <div className="landing-deploy-copy landing-reveal-copy" data-landing-reveal>
              <span>{text.login.deployEyebrow}</span>
              <h2>{text.login.deployTitle}</h2>
              <p>{text.login.deployDesc}</p>
            </div>
            <div
              className="landing-deploy-panel landing-reveal-panel"
              data-landing-reveal
              aria-label={text.login.deployTitle}
            >
              <a
                href="https://github.com/hloolx/HloolMail"
                target="_blank"
                rel="noopener noreferrer"
              >
                <Github size={17} />
                <span>{text.login.deployGithub}</span>
                <ArrowRight size={15} />
              </a>
              <div className="landing-deploy-method">
                <Terminal size={17} />
                <div>
                  <b>{text.login.deployBinary}</b>
                  <code>./hlool-mail serve</code>
                </div>
              </div>
              <div className="landing-deploy-method">
                <Code2 size={17} />
                <div>
                  <b>{text.login.deployDocker}</b>
                  <code>docker compose up -d</code>
                </div>
              </div>
            </div>
          </section>

          <footer className="landing-footer">
            <div className="landing-footer-inner">
              <div className="landing-footer-brandcol">
                <div className="landing-footer-brand">
                  <span className="app-header-brand-mark">
                    <AppLogo />
                  </span>
                  <span>HLOOL Mail</span>
                </div>
                <p className="landing-footer-tagline">{text.login.footerCopy}</p>
              </div>
              <nav className="landing-footer-links" aria-label={text.login.deployGithub}>
                <a
                  className="landing-footer-repo"
                  href="https://github.com/hloolx/HloolMail"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <Github size={16} />
                  <span>hloolx/HloolMail</span>
                  <ArrowRight size={14} />
                </a>
              </nav>
            </div>
            <div className="landing-footer-bottom">
              <span>&copy; {new Date().getFullYear()} HLOOL Mail</span>
            </div>
          </footer>
        </>
      }
    </div>
  );
}

type LandingStatProps = {
  icon: LucideIcon;
  value: number;
  label: string;
  loading: boolean;
};

const LANDING_STAT_BARS = 11;

function landingStatBars(seedKey: string): number[] {
  // Deterministic pseudo-random heights so the bars are stable across renders,
  // but distinct per stat. Purely decorative, no real data.
  let seed = 0;
  for (let i = 0; i < seedKey.length; i += 1) seed = (seed * 31 + seedKey.charCodeAt(i)) >>> 0;
  const bars: number[] = [];
  let prev = 0.5;
  for (let i = 0; i < LANDING_STAT_BARS; i += 1) {
    seed = (seed * 1103515245 + 12345) & 0x7fffffff;
    const next = 0.32 + ((seed % 1000) / 1000) * 0.5;
    // smooth the series so the sparkline reads as an upward trend
    prev = Math.min(1, Math.max(0.22, prev * 0.45 + next * 0.55 + i * 0.022));
    bars.push(prev);
  }
  return bars;
}

function LandingStat({ icon: Icon, value, label, loading }: LandingStatProps) {
  const animatedValue = useCountUp(loading ? 0 : value);
  const displayValue = loading ? 0 : animatedValue;
  const bars = useMemo(() => landingStatBars(label), [label]);
  return (
    <div className={`landing-stat ${loading ? 'landing-stat-loading' : 'landing-stat-ready'}`}>
      <Icon size={20} />
      <b
        className="landing-stat-number"
        key={`${loading ? 'loading' : 'ready'}-${label}-${value}`}
        aria-busy={loading}
      >
        {displayValue.toLocaleString()}
      </b>
      <span>{label}</span>
      <div className="landing-stat-spark" aria-hidden="true">
        {bars.map((height, index) => (
          <span key={index} style={{ height: `${Math.round(height * 100)}%` }} />
        ))}
      </div>
    </div>
  );
}
